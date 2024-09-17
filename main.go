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
	Grey         = color.New(color.FgHiBlack).SprintFunc()
)

type Config struct {
	UseProxies  bool   `json:"use_proxies"`
	ProxyFile   string `json:"proxy_file"`
	PromoFile   string `json:"promo_file"`
	ThreadCount int    `json:"thread_count"`
	RetryCount  int    `json:"retry_count"`
	PromoCount  int    `json:"promo_count"`
	UserToken   string `json:"token"`
}

func clear() {
	fmt.Print("\033[H\033[2J")
}

var (
	randSource     = rand.NewSource(time.Now().UnixNano())
	randGen        = rand.New(randSource)
	generatedNames = make(map[string]struct{})
	namesMutex     sync.Mutex
	letterRunes    = []rune("abcdefghijklmnopqrstuvwxyz")
)

func generateName() string {
	var fullName strings.Builder

	for {
		fullName.Reset()

		nameLength := randGen.Intn(3) + 3

		for i := 0; i < nameLength; i++ {
			fullName.WriteRune(letterRunes[randGen.Intn(len(letterRunes))])
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
	hits        int
	invalids    int
	errors      int
	retries     int
	onemonth    int
	threemonths int
	oneyear     int
	startTime   time.Time
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

func updateTitleChecker() {
	elapsedTime := time.Since(startTime)
	hours := int(elapsedTime.Hours())
	minutes := int(elapsedTime.Minutes()) % 60
	seconds := int(elapsedTime.Seconds()) % 60

	elapsedTimeStr := fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)

	title := fmt.Sprintf("Promo Checker | 1 Month: %d - 3 Months: %d - 1 Year: %d | Invalid/Claimed: %d | Retries: %d | Errors: %d | Elapsed Time: %s",
		onemonth, threemonths, oneyear, invalids, retries, errors, elapsedTimeStr)

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

var (
	generatedCodes = make(map[string]struct{})
	codesMutex     sync.Mutex
)

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
			} else {
				invalids++
				continue
			}

			codesMutex.Lock()
			if _, exists := generatedCodes[codeValue]; exists {
				codesMutex.Unlock()
				continue
			}
			generatedCodes[codeValue] = struct{}{}
			codesMutex.Unlock()

			hits++

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

func checker(proxies []string, code string) {
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

	tukan := config.UserToken
	referer := fmt.Sprintf("https://discord.com/billing/promotions/%s", code)

	headers := map[string]string{
		"sec-ch-ua":          `"Chromium";v="128", "Not;A=Brand";v="24", "Google Chrome";v="128"`,
		"x-super-properties": `eyJvcyI6IldpbmRvd3MiLCJicm93c2VyIjoiQ2hyb21lIiwiZGV2aWNlIjoiIiwic3lzdGVtX2xvY2FsZSI6ImVuLVVTIiwiYnJvd3Nlcl91c2VyX2FnZW50IjoiTW96aWxsYS81LjAgKFdpbmRvd3MgTlQgMTAuMDsgV2luNjQ7IHg2NCkgQXBwbGVXZWJLaXQvNTM3LjM2IChLSFRNTCwgbGlrZSBHZWNrbykgQ2hyb21lLzEyOC4wLjAuMCBTYWZhcmkvNTM3LjM2IiwiYnJvd3Nlcl92ZXJzaW9uIjoiMTI4LjAuMC4wIiwib3NfdmVyc2lvbiI6IjEwIiwicmVmZXJyZXIiOiJodHRwczovL2NyYWNrZWQuaW8vIiwicmVmZXJyaW5nX2RvbWFpbiI6ImNyYWNrZWQuaW8iLCJyZWZlcnJpbmdfZG9tYWluIjoiIiwicmVmZXJyaW5nX2RvbWFpbl9jdXJyZW50IjoiIiwicmVsZWFzZV9jaGFubmVsIjoic3RhYmxlIiwiY2xpZW50X2J1aWxkX251bWJlciI6MzI2ODU0LCJjbGllbnRfZXZlbnRfc291cmNlIjpudWxsfQ==`,
		"x-debug-options":    "bugReporterEnabled",
		"sec-ch-ua-mobile":   "?0",
		"authorization":      tukan,
		"user-agent":         "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36",
		"x-discord-timezone": "Europe/Bucharest",
		"x-discord-locale":   "en-US",
		"sec-ch-ua-platform": `"Windows"`,
		"accept":             "*/*",
		"sec-fetch-site":     "same-origin",
		"sec-fetch-mode":     "cors",
		"sec-fetch-dest":     "empty",
		"referer":            referer,
		"accept-encoding":    "gzip, deflate, br, zstd",
		"accept-language":    "en-US,en;q=0.9",
	}

	maxRetries := config.RetryCount

	for attempt := 0; attempt <= maxRetries; attempt++ {
		var proxyURL *url.URL
		if len(proxies) > 0 {
			uuidID := uint64(uuid.New().ID())
			proxyString := proxies[uuidID%uint64(len(proxies))]
			var err error
			proxyURL, err = url.Parse(proxyString)
			if err != nil {
				fmt.Println("Invalid proxy URL:", proxyString)
				continue
			}
		}

		promoUrl := fmt.Sprintf("https://discord.com/api/v9/entitlements/gift-codes/%s?country_code=RO&with_application=false&with_subscription_plan=true", code)

		req, err := http.NewRequest("GET", promoUrl, nil)
		if err != nil {
			//fmt.Println("Error creating request:", err)
			errors++
			return
		}

		for k, v := range headers {
			req.Header.Set(k, v)
		}

		if proxyURL != nil {
			client.Transport = &http.Transport{Proxy: http.ProxyURL(proxyURL)}
		}

		resp, err := client.Do(req)
		if err != nil {
			//fmt.Println("Error sending request:", err)
			errors++
			continue
		}

		defer func() {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()

		timedate := Grey(time.Now().Format("15:04:05"))
		codeC := Magenta(code)

		if resp.StatusCode == http.StatusTooManyRequests {
			var rateLimitResp struct {
				Message    string  `json:"message"`
				RetryAfter float64 `json:"retry_after"`
				Global     bool    `json:"global"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&rateLimitResp); err != nil {
				//fmt.Println("Error decoding rate limit response:", err)
				errors++
				continue
			}

			if rateLimitResp.Message == "You are being rate limited." {
				retries++
				sleepDuration := time.Duration(rateLimitResp.RetryAfter * float64(time.Second))
				fmt.Printf("[ %s ] %s [ %s ] %s %s %s\n", timedate, Grey(">"), Yellow("!"), Yellow(codeC), Grey("| Retrying in:"), sleepDuration)
				time.Sleep(sleepDuration)
				attempt--
				continue
			}
		}

		//fmt.Printf("Response Status Code: %d\n", resp.StatusCode)

		contentEncoding := resp.Header.Get("Content-Encoding")
		bodyStr, err := decompressBody(resp.Body, contentEncoding)
		if err != nil {
			fmt.Println("Error decompressing body:", err)
			errors++
			continue
		}

		//fmt.Println(bodyStr)

		re1 := regexp.MustCompile(`"subscription_plan":\s*\{[^{}]*"name":"([^"]+)"`)
		matches1 := re1.FindAllStringSubmatch(bodyStr, -1)

		var nitro string
		if len(matches1) > 0 && len(matches1[0]) > 1 {
			nitro = matches1[0][1]
		}

		var nitroLen string
		if nitro == "Nitro Monthly" {
			nitroLen = LightGreen("1 Month")
		} else if nitro == "Nitro Yearly" {
			nitroLen = LightGreen("1 Year")
		} else {
			nitroLen = LightGreen("3 Months")
		}

		re := regexp.MustCompile(`"uses":([^"]+),`)
		matches := re.FindAllStringSubmatch(bodyStr, -1)

		var used string
		if len(matches) > 0 && len(matches[0]) > 1 {
			used = matches[0][1]
		}

		var ynk string
		if strings.Contains(bodyStr, "Unknown Gift Code") {
			ynk = Red("Unknown Promo")
			invalids++
			fmt.Printf("[ %s ] %s [ %s ] %s - %s\n", timedate, Grey(">"), Red("-"), ynk, codeC)
		}

		var usedStr2 string
		usedStr := string(used)
		if usedStr == "1" {
			usedStr2 = Red("Used Promo")
			invalids++
			fmt.Printf("[ %s ] %s [ %s ] %s - %s\n", timedate, Grey(">"), Red("-"), usedStr2, codeC)
		}

		if usedStr == "0" && nitro == "Nitro Monthly" {
			onemonth++
			usedStr2 = LightGreen("Valid Promo")
			fmt.Printf("[ %s ] %s [ %s ] %s - %s | %s\n", timedate, Grey(">"), LightGreen("+"), usedStr2, codeC, nitroLen)
			appendToFile("results/1 Month.txt", fmt.Sprintf("https://promos.discord.gg/%s\n", code))
		}
		if usedStr == "0" && nitro == "Nitro Yearly" {
			oneyear++
			usedStr2 = LightGreen("Valid Promo")
			fmt.Printf("[ %s ] %s [ %s ] %s - %s | %s\n", timedate, Grey(">"), LightGreen("+"), usedStr2, codeC, nitroLen)
			appendToFile("results/1 Year.txt", fmt.Sprintf("https://promos.discord.gg/%s\n", code))
		}
		if usedStr == "0" && (nitro != "Nitro Monthly" && nitro != "Nitro Yearly") {
			threemonths++
			usedStr2 = LightGreen("Valid Promo")
			fmt.Printf("[ %s ] %s [ %s ] %s - %s | %s\n", timedate, Grey(">"), LightGreen("+"), usedStr2, codeC, nitroLen)
			appendToFile("results/3 Months.txt", fmt.Sprintf("https://promos.discord.gg/%s\n", code))
		}

		return
	}
}

func start() {
	clear()
	logo()

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

func startchecker() {
	clear()
	logo()

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

	promos := config.PromoFile
	file, err := os.Open(promos)
	if err != nil {
		fmt.Println("Error opening promo file:", err)
		return
	}
	defer file.Close()

	var codes []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		code := scanner.Text()
		code = strings.TrimPrefix(code, "https://promos.discord.gg/")
		codes = append(codes, code)
	}

	var proxies []string
	if config.UseProxies {
		proxies, err = getProxiesFromFile(config.ProxyFile)
		if err != nil {
			fmt.Println("Error opening proxies file:", err)
			return
		}
	}

	totalCodes := len(codes)
	startTime = time.Now()

	resultsChan := make(chan struct{}, totalCodes)
	wg := &sync.WaitGroup{}
	sem := make(chan struct{}, config.ThreadCount)

	for _, code := range codes {
		wg.Add(1)
		sem <- struct{}{}

		go func(code string) {
			defer wg.Done()
			defer func() { <-sem }()

			defer func() {
				if r := recover(); r != nil {
					fmt.Println(r)
				}
			}()

			checker(proxies, code)
			resultsChan <- struct{}{}
		}(code)

		updateTitleChecker()
	}

	wg.Wait()
	close(resultsChan)
	close(sem)
}

func logo() {
	fmt.Println(Magenta("	  ┓ ┏┓┳┓┓ 		cracked.io/knzz0"))
	fmt.Println(Magenta("	 •┃ ┣┫┃┃┃ 		t.me/knzz0"))
	fmt.Println(Magenta("	 •┛ ┛┗┻┛┻ 		github.com/ad1txx1"))
}

func main() {
	clear()
	logo()

	setConsoleTitle("Hi - t.me/knzz0 | github.com/ad1txx1 | cracked.io/knzz0")

	fmt.Printf(`
	%s%s1%s%s Promo Gen
	%s%s2%s%s Promo Checker
	%s%s3%s%s Exit
	`, Magenta("["), Reset(), Magenta("]"), Reset(),
		Magenta("["), Reset(), Magenta("]"), Reset(),
		Magenta("["), Reset(), Magenta("]"), Reset())

	fmt.Println()
	fmt.Print(Magenta("\n        > Enter choice: "))

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	choice := scanner.Text()

	switch choice {
	case "1":
		start()
	case "2":
		startchecker()
	case "3":
		os.Exit(0)
	default:
		fmt.Printf("%s	[x] Please enter a valid choice.%s\n", Red(), Reset())
		time.Sleep(1 * time.Second)
		main()
	}
}
