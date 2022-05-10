package main

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	baseURL = "https://2022electionresults.comelec.gov.ph"
)

var (
	client *http.Client
)

func init() {
	client = &http.Client{}
}

func addHeaders(r *http.Request) {
	r.Header.Add("Accept", "application/json, text/plain, */*")
	r.Header.Add("Accept-Encoding", "gzip, deflate")
	r.Header.Add("Cache-Control", "no-cache")
	r.Header.Add("Connection", "keep-alive")
	r.Header.Add("Host", "2022electionresults.comelec.gov.ph")
	r.Header.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:100.0) Gecko/20100101 Firefox/100.0")
}

func download(localPath, URLPath string) error {
	if _, err := os.Stat(localPath); errors.Is(err, os.ErrNotExist) {
		fmt.Println("Downloading: ", URLPath)
		f, err := os.Create(localPath)
		if err != nil {
			return err
		}
		defer f.Close()

		req, err := http.NewRequest("GET", baseURL+URLPath, nil)
		if err != nil {
			return err
		}
		addHeaders(req)

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return errors.New("Error downloading: " + resp.Status)
		}
		if resp.Header.Get("Content-Length") == "0" {
			return errors.New("Empty response")
		}
		var reader io.ReadCloser
		switch resp.Header.Get("Content-Encoding") {
		case "gzip":
			reader, err = gzip.NewReader(resp.Body)
			if err != nil {
				return err
			}
			defer reader.Close()
		default:
			reader = resp.Body
		}

		n, err := io.Copy(f, reader)
		if err != nil {
			return err
		}
		if n == 0 {
			defer os.Remove(localPath) // nolint
		}

		time.Sleep(500 * time.Millisecond)
	}

	return nil
}

func Children(file string) ([]string, string, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	outIfce := make(map[string]interface{})
	err = json.NewDecoder(f).Decode(&outIfce)
	if err != nil {
		// delete file if not valid json
		log.Print(err, " Removing file ", file)
		err = os.Remove(file)
		return nil, "", err
	}

	var (
		outCodes []string
		outType string
	)
	if v, ok := outIfce["srs"]; ok {
		switch vv := v.(type) {
		case map[string]interface{}:
			for k, _ := range vv {
				outCodes = append(outCodes, k)
			}
			if len(outCodes) > 0 {
				switch vvv := vv[outCodes[0]].(type) {
				case map[string]interface{}:
					outType = vvv["can"].(string)
				}
			}
		}
	}

	return outCodes, outType, err
}

func downloadChildren(file string, dlChan chan []string) {
	children, outType, err := Children(file)
	if err != nil {
		log.Print(err)
		return
	}

	dir := "output/" + outType + "/"
	err = os.MkdirAll(dir, 0777)
	if err != nil {
		log.Print(err)
		return
	}

	for _, r := range children {
		code := r[0:2]
		dlChan <- []string{dir + r + ".json", "/data/regions/" + code + "/" + r + ".json"}
	}

	for _, r := range children {
		downloadChildren(dir + r + ".json", dlChan)
	}
}

func main() {
	// download root
	err := download("output/root.json", "/data/regions/root.json")
	if err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup
	dlChan := make(chan []string)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			for dl := range dlChan {
				err := download(dl[0], dl[1])
				if err != nil {
					log.Print(err)
				}
			}
			wg.Done()
		}()
	}

	downloadChildren("output/root.json", dlChan)

	close(dlChan)
	wg.Wait()
}
