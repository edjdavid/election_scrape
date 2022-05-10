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
	"strconv"
	"strings"
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

		f, err := os.Create(localPath)
		if err != nil {
			return err
		}
		defer f.Close()

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
		outType  string
	)
	if v, ok := outIfce["srs"]; ok {
		switch vv := v.(type) {
		case map[string]interface{}:
			for _, vvv := range vv {
				switch vvvv := vvv.(type) {
				case map[string]interface{}:
					outCodes = append(outCodes, vvvv["url"].(string))
				}
			}
			for _, v := range vv {
				switch vvv := v.(type) {
				case map[string]interface{}:
					outType = vvv["can"].(string)
				}
				break
			}
		}
	}

	return outCodes, outType, err
}

func downloadChildren(file string) {
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

	for _, r := range children {
		code := strings.Split(r, "/")[1]
		dlChan <- []string{dir + code + ".json", "/data/regions/" + r + ".json"}
	}

	close(dlChan)
	wg.Wait()

	for _, r := range children {
		code := strings.Split(r, "/")[1]
		downloadChildren(dir + code + ".json")
	}
}

func downloadPrecint() {
	err := os.MkdirAll("output/Contest/", 0777)
	if err != nil {
		log.Fatal(err)
	}
	dir := "output/Barangay/"
	osdir, err := os.Open(dir)
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

	candidateMap := make(map[int]bool)
	for {
		files, err := osdir.Readdirnames(10)
		if err != nil {
			log.Print(err)
			if errors.Is(err, io.EOF) {
				close(dlChan)
			}
			break
		}

		for _, f := range files {
			bfile, err := os.Open(dir + f)
			if err != nil {
				log.Print(err)
				continue
			}
			var out map[string]interface{}
			err = json.NewDecoder(bfile).Decode(&out)
			if err != nil {
				log.Print(err)
				continue
			}

			for _, pps := range out["pps"].([]interface{}) {
				for _, vbsi := range pps.(map[string]interface{})["vbs"].([]interface{}) {
					switch vbs := vbsi.(type) {
					case map[string]interface{}:
						url := vbs["url"].(string)
						err = os.MkdirAll("output/Precint/"+strings.Split(url, "/")[0], 0777)
						if err != nil {
							log.Print(err)
							continue
						}
						dlChan <- []string{"output/Precint/"+url+".json", "/data/results/"+url+".json"}
						for _, csi := range vbs["cs"].([]interface{}) {
							cs := int(csi.(float64))
							if !candidateMap[cs] {
								id := strconv.Itoa(cs)
								dlChan <- []string{"output/Contest/"+id+".json", "/data/contests/"+id+".json"}
								candidateMap[cs] = true
							}
						}
					}
				}
			}
		}
	}

	wg.Wait()
}

func main() {
	// download root
	err := download("output/root.json", "/data/regions/root.json")
	if err != nil {
		log.Fatal(err)
	}

	downloadChildren("output/root.json")
	downloadPrecint()
}
