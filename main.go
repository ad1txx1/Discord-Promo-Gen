package main

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/fatih/color"
	"github.com/google/uuid"
)

var (
	Reset        = color.New(color.Reset).SprintFunc()
	Black        = color.New(color.FgBlack).SprintFunc()
	Red          = color.New(color.FgRed).SprintFunc()
	Green        = color.New(color.FgGreen).SprintFunc()
	Yellow       = color.New(color.FgYellow).SprintFunc()
	Blue         = color.New(color.FgBlue).SprintFunc()
	Magenta      = color.New(color.FgMagenta).SprintFunc()
	Cyan         = color.New(color.FgCyan).SprintFunc()
	White        = color.New(color.FgWhite).SprintFunc()
	LightRed     = color.New(color.FgHiRed).SprintFunc()
	LightGreen   = color.New(color.FgHiGreen).SprintFunc()
	LightBlue    = color.New(color.FgHiBlue).SprintFunc()
	LightMagenta = color.New(color.FgHiMagenta).SprintFunc()
	LightCyan    = color.New(color.FgHiCyan).SprintFunc()
)

type Config struct {
	UseProxies  bool   `json:"use_proxies"`
	ProxyFile   string `json:"proxy_file"`
	ThreadCount int    `json:"thread_count"`
	RetryCount  int    `json:"retry_count"`
	PromoCount  int    `json:"promo_count"`
}

func clear() {
	fmt.Print("\033[H\033[2J")
}

var (
	generatedNames = make(map[string]struct{})
	namesMutex     sync.Mutex
)

func generateName() string {
	letterRunes := []rune("abcdefghijklmnopqrstuvwxyz")
	rand.Seed(time.Now().UnixNano())

	var fullName strings.Builder

	for {
		nameLength := rand.Intn(2) + 3

		fullName.Reset()
		for i := 0; i < nameLength; i++ {
			fullName.WriteRune(letterRunes[rand.Intn(len(letterRunes))])
		}

		newName := fullName.String()

		namesMutex.Lock()
		if _, exists := generatedNames[newName]; !exists {
			generatedNames[newName] = struct{}{}
			namesMutex.Unlock()
			return newName
		}
		namesMutex.Unlock()
	}
}

func getProxiesFromFile(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var proxies []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		proxies = append(proxies, scanner.Text())
	}
	return proxies, scanner.Err()
}

func setConsoleTitle(title string) {
	ptr, _ := syscall.UTF16PtrFromString(title)
	syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleTitleW").Call(uintptr(unsafe.Pointer(ptr)))
}

var (
	hits      int
	invalids  int
	errors    int
	retries   int
	startTime time.Time
)

func init() {
	startTime = time.Now()
}

func updateTitle() {
	elapsedTime := time.Since(startTime)
	hours := int(elapsedTime.Hours())
	minutes := int(elapsedTime.Minutes()) % 60
	seconds := int(elapsedTime.Seconds()) % 60

	elapsedTimeStr := fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)

	title := fmt.Sprintf("Promo Gen | Generated: %d | Failed: %d | Retries: %d | Errors: %d | Elapsed Time: %s", hits, invalids, retries, errors, elapsedTimeStr)

	setConsoleTitle(title)
}

func decompressBody(body io.Reader, encoding string) (string, error) {
	var reader io.Reader = body

	switch encoding {
	case "gzip":
		gzipReader, err := gzip.NewReader(body)
		if err != nil {
			return "", err
		}
		defer gzipReader.Close()
		reader = gzipReader
	case "deflate":
		reader = flate.NewReader(body)
	}

	bodyBytes, err := ioutil.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(bodyBytes), nil
}

var client = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

func appendToFile(filePath, content string) {
	if err := ensureDirExists("results"); err != nil {
		fmt.Println("Error creating folder:", err)
		return
	}

	if err := ensureFileExists(filePath); err != nil {
		fmt.Println("Error creating file:", err)
		return
	}

	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, os.ModeAppend)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer f.Close()

	formattedContent := strings.TrimSpace(content)
	if _, err := f.WriteString(formattedContent + "\n"); err != nil {
		fmt.Println("Error writing file:", err)
	}
}

func ensureDirExists(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return os.MkdirAll(dir, os.ModePerm)
	}
	return nil
}

func ensureFileExists(file string) error {
	if _, err := os.Stat(file); os.IsNotExist(err) {
		_, err := os.Create(file)
		return err
	}
	return nil
}

func getUuid(proxies []string) string {
	headers := map[string]string{
		"accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
		"accept-language":           "en-US,en;q=0.9",
		"cache-control":             "max-age=0",
		"priority":                  "u=0, i",
		"referer":                   "https://www.chess.com/friends?name=csolver.xyz",
		"sec-ch-ua":                 `"Chromium";v="128", "Not;A=Brand";v="24", "Microsoft Edge";v="128"`,
		"sec-ch-ua-mobile":          "?0",
		"sec-ch-ua-platform":        `"Windows"`,
		"sec-fetch-dest":            "document",
		"sec-fetch-mode":            "navigate",
		"sec-fetch-site":            "same-origin",
		"sec-fetch-user":            "?1",
		"upgrade-insecure-requests": "1",
		"user-agent":                "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
	}

	configFile := "config.json"
	configFileContent, err := ioutil.ReadFile(configFile)
	if err != nil {
		errors++
	}

	var config Config
	err = json.Unmarshal(configFileContent, &config)
	if err != nil {
		errors++
	}

	randomName := generateName()
	urlStr := fmt.Sprintf("https://www.chess.com/member/%s", randomName)
	maxRetries := config.RetryCount

	for attempt := 0; attempt <= maxRetries; attempt++ {
		var proxyURL *url.URL
		if len(proxies) > 0 {
			uuidID := uint64(uuid.New().ID())
			proxyString := proxies[uuidID%uint64(len(proxies))]
			var err error
			proxyURL, err = url.Parse(proxyString)
			if err != nil {
				errors++
			}
		}

		req, err := http.NewRequest("GET", urlStr, nil)
		if err != nil {
			errors++
		}

		for k, v := range headers {
			req.Header.Set(k, v)
		}

		if proxyURL != nil {
			client.Transport.(*http.Transport).Proxy = http.ProxyURL(proxyURL)
		}

		resp, err := client.Do(req)
		if err != nil {
			errors++
		}

		defer func() {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()

		contentEncoding := resp.Header.Get("Content-Encoding")
		bodyStr, err := decompressBody(resp.Body, contentEncoding)
		if err != nil {
			fmt.Println(err)
		}

		re := regexp.MustCompile(`data-user-uuid="([^"]+)"`)
		matches := re.FindAllStringSubmatch(bodyStr, -1)

		if len(matches) > 0 && len(matches[0]) > 1 {
			return matches[0][1]
		}
	}

	return ""
}

func gen(proxies []string, promoTarget *int32, resultsChan chan struct{}, wg *sync.WaitGroup, sem chan struct{}) {
	defer wg.Done()
	defer func() { <-sem }()
	defer func() {
		if r := recover(); r != nil {
		}
	}()

	configFile := "config.json"
	configFileContent, err := ioutil.ReadFile(configFile)
	if err != nil {
		errors++
		return
	}

	var config Config
	err = json.Unmarshal(configFileContent, &config)
	if err != nil {
		errors++
		return
	}

	uuidstr := getUuid(proxies)
	if uuidstr == "" {
		errors++
		return
	}

	uuidstrShort := fmt.Sprintf("%s...", uuidstr[:len(uuidstr)-23])

	headers := map[string]string{
		"accept":             "application/json, text/plain, */*",
		"accept-language":    "en-US,en;q=0.9",
		"content-type":       "application/json",
		"origin":             "https://www.chess.com",
		"priority":           "u=1, i",
		"referer":            "https://www.chess.com/play/computer/discord-wumpus?utm_source=partnership&utm_medium=article&utm_campaign=discord2024_bot",
		"sec-ch-ua":          `"Chromium";v="128", "Not;A=Brand";v="24", "Microsoft Edge";v="128"`,
		"sec-ch-ua-mobile":   "?0",
		"sec-ch-ua-platform": `"Windows"`,
		"sec-fetch-dest":     "empty",
		"sec-fetch-mode":     "cors",
		"sec-fetch-site":     "same-origin",
		"user-agent":         "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
	}

	data := map[string]string{
		"userUuid":   uuidstr,
		"campaignId": "4daf403e-66eb-11ef-96ab-ad0a069940ce",
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		errors++
		return
	}

	maxRetries := config.RetryCount
	retryDelay := 2 * time.Second

	for {
		if atomic.LoadInt32(promoTarget) <= 0 {
			return
		}

		var proxyURL *url.URL
		if len(proxies) > 0 {
			uuidID := uint64(uuid.New().ID())
			proxyString := proxies[uuidID%uint64(len(proxies))]
			proxyURL, err = url.Parse(proxyString)
			if err != nil {
				errors++
				continue
			}
		}

		req, err := http.NewRequest("POST", "https://www.chess.com/rpc/chesscom.partnership_offer_codes.v1.PartnershipOfferCodesService/RetrieveOfferCode", bytes.NewBuffer(jsonData))
		if err != nil {
			errors++
			continue
		}

		for k, v := range headers {
			req.Header.Set(k, v)
		}

		if proxyURL != nil {
			client.Transport.(*http.Transport).Proxy = http.ProxyURL(proxyURL)
		}

		var resp *http.Response
		var responseErr error
		for attempt := 0; attempt <= maxRetries; attempt++ {
			resp, responseErr = client.Do(req)
			if responseErr != nil {
				errors++
				break
			}

			if resp.StatusCode == http.StatusTooManyRequests {
				retries++
				time.Sleep(retryDelay)
				continue
			}

			contentEncoding := resp.Header.Get("Content-Encoding")
			bodyStr, err := decompressBody(resp.Body, contentEncoding)
			if err != nil {
				errors++
				continue
			}

			re := regexp.MustCompile(`"codeValue":"([^"]+)"`)
			matches := re.FindAllStringSubmatch(bodyStr, -1)

			var codeValue string
			if len(matches) > 0 && len(matches[0]) > 1 {
				codeValue = matches[0][1]
				hits++
			} else {
				invalids++
			}

			creating := Yellow("[Creating Promo]")
			arrow := Magenta("----->")
			uuidColor := Yellow(uuidstrShort)
			creatingPromo := fmt.Sprintf("[%s] %s %s %s", time.Now().Format("15:04:05"), creating, arrow, uuidColor)
			fmt.Println(creatingPromo)

			generated := LightGreen("[Generated Promo]")
			linkColor := LightGreen("https://promos.discord.gg/")
			codeValueColor := LightGreen(codeValue)
			createdPromo := fmt.Sprintf("[%s] %s %s %s%s", time.Now().Format("15:04:05"), generated, arrow, linkColor, codeValueColor)
			fmt.Println(createdPromo)
			appendToFile("results/promos.txt", fmt.Sprintf("https://promos.discord.gg/%s\n", codeValue))

			atomic.AddInt32(promoTarget, -1)

			resultsChan <- struct{}{}
			break
		}

	}
}

func start() {
	clear()

	configFile := "config.json"
	configFileContent, err := ioutil.ReadFile(configFile)
	if err != nil {
		fmt.Println("Error reading config file:", err)
		return
	}

	var config Config
	err = json.Unmarshal(configFileContent, &config)
	if err != nil {
		fmt.Println("Error parsing config file:", err)
		return
	}

	var proxies []string
	if config.UseProxies {
		proxies, err = getProxiesFromFile(config.ProxyFile)
		if err != nil {
			fmt.Println("Error opening proxies file:", err)
			return
		}
	}

	resultsChan := make(chan struct{})
	wg := &sync.WaitGroup{}
	sem := make(chan struct{}, config.ThreadCount)

	var promoTarget int32 = int32(config.PromoCount)

	go func() {
		for range resultsChan {
			updateTitle()
		}
	}()

	for i := 0; i < config.ThreadCount; i++ {
		wg.Add(1)
		sem <- struct{}{}

		go gen(proxies, &promoTarget, resultsChan, wg, sem)
	}

	wg.Wait()
	close(resultsChan)

}

func main() {
	start()
}
