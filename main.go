package main

import (
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"fmt"
        "gopkg.in/yaml.v2"
)

type Metric struct {
	Measurement string
	Tags        map[string]string
	Fields      map[string]interface{}
	Time        time.Time
}

func (p *Metric) ShortString() string {
        msg := p.Measurement
        for k, v := range p.Tags {
                msg += fmt.Sprintf(",%v=%v", k, v)
        }
        msg += " "
        for k, v := range p.Fields {
                msg += fmt.Sprintf("%v=%v,", k, v)
        }
        msg = msg[:len(msg)-1]
        return msg
}

func (p *Metric) String() string {
	msg := p.ShortString()
        msg += fmt.Sprintf(" %v", p.Time)
	return msg
}

// Accumulator defines a mocked out accumulator
type Accumulator struct {
	Metrics  []*Metric
}

// AddFields adds a measurement point with a specified timestamp.
func (a *Accumulator) AddFields(
	measurement string,
	fields map[string]interface{},
	tags map[string]string,
	timestamp ...time.Time,
) {
	if tags == nil {
		tags = map[string]string{}
	}

	if len(fields) == 0 {
		return
	}

	var t time.Time
	if len(timestamp) > 0 {
		t = timestamp[0]
	} else {
		t = time.Now()
	}

	p := &Metric{
		Measurement: measurement,
		Fields:      fields,
		Tags:        tags,
		Time:        t,
	}

	a.Metrics = append(a.Metrics, p)
}

func (a *Accumulator) Post (url string) {
        resp, err := http.Post(url, "application/x-www-form-urlencoded", strings.NewReader("tos=" + tos + "&content=" + content))

        if err != nil {
                return nil, err
        }

        defer resp.Body.Close()
        return ioutil.ReadAll(resp.Body)


	for _, v := range a.Metrics {
		
		fmt.Printf("%v\n", v.ShortString())
		curl -XPOST '139.199.73.164:32003/write?db=ops'
	}
}

func (a *Accumulator) Print () {
	for _, v := range a.Metrics {
		fmt.Printf("%v\n", v.ShortString())
	}
}

// HTTPResponse struct
type HTTPResponse struct {
	Address             string
	Body                string
	Method              string
	ResponseTimeout     time.Duration
	Headers             map[string]string
	FollowRedirects     bool
	ResponseStringMatch string
	compiledStringMatch *regexp.Regexp

	// Path to CA file
	SSLCA string `toml:"ssl_ca"`
	// Path to host cert file
	SSLCert string `toml:"ssl_cert"`
	// Path to cert key file
	SSLKey string `toml:"ssl_key"`
	// Use SSL but skip chain & host verification
	InsecureSkipVerify bool
}

var sampleConfig = `
  address:
  - url: "http://github.com"
    method: "GET"
    response_timeout = "5s"
`

// SampleConfig returns the plugin SampleConfig
func (h *HTTPResponse) SampleConfig() string {
	return sampleConfig
}

type InfluxDB struct {
	Url			string	`yaml:"url"`
	Database		string	`yaml:"database"`
	Username		string	`yaml:"username"`
	Password		string	`yaml:"password"`
}

type HttpAddress struct {
	Url			string	`yaml:"url"`
	Method			string	`yaml:"method"`
	ResponseTimeout		string	`yaml:"response_timeout"`
}

type Config struct {
	InfluxDB	[]*HttpAddress	`yaml:"influxdb"`
	Addresses	[]*HttpAddress	`yaml:"address"`
}

// ErrRedirectAttempted indicates that a redirect occurred
var ErrRedirectAttempted = errors.New("redirect")

// CreateHttpClient creates an http client which will timeout at the specified
// timeout period and can follow redirects if specified
func (h *HTTPResponse) createHttpClient() (*http.Client, error) {
	tr := &http.Transport{
		ResponseHeaderTimeout: h.ResponseTimeout,
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   h.ResponseTimeout,
	}

	if h.FollowRedirects == false {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return ErrRedirectAttempted
		}
	}
	return client, nil
}

// HTTPGather gathers all fields and returns any errors it encounters
func (h *HTTPResponse) HTTPGather() (map[string]interface{}, error) {
	// Prepare fields
	fields := make(map[string]interface{})

	client, err := h.createHttpClient()
	if err != nil {
		return nil, err
	}

	var body io.Reader
	if h.Body != "" {
		body = strings.NewReader(h.Body)
	}
	request, err := http.NewRequest(h.Method, h.Address, body)
	if err != nil {
		return nil, err
	}

	for key, val := range h.Headers {
		request.Header.Add(key, val)
		if key == "Host" {
			request.Host = val
		}
	}

	// Start Timer
	start := time.Now()
	resp, err := client.Do(request)
	if err != nil {
		if h.FollowRedirects {
			return nil, err
		}
		if urlError, ok := err.(*url.Error); ok &&
			urlError.Err == ErrRedirectAttempted {
			err = nil
		} else {
			return nil, err
		}
	}
	fields["response_time"] = time.Since(start).Seconds()
	fields["http_response_code"] = resp.StatusCode

	// Check the response for a regex match.
	if h.ResponseStringMatch != "" {

		// Compile once and reuse
		if h.compiledStringMatch == nil {
			h.compiledStringMatch = regexp.MustCompile(h.ResponseStringMatch)
			if err != nil {
				log.Printf("E! Failed to compile regular expression %s : %s", h.ResponseStringMatch, err)
				fields["response_string_match"] = 0
				return fields, nil
			}
		}

		bodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Printf("E! Failed to read body of HTTP Response : %s", err)
			fields["response_string_match"] = 0
			return fields, nil
		}

		if h.compiledStringMatch.Match(bodyBytes) {
			fields["response_string_match"] = 1
		} else {
			fields["response_string_match"] = 0
		}

	}

	return fields, nil
}

// Gather gets all metric fields and tags and returns any errors it encounters
func (h *HTTPResponse) Gather(acc *Accumulator) error {
	// Set default values
	if h.ResponseTimeout < time.Second {
		h.ResponseTimeout = time.Second * 5
	}
	// Check send and expected string
	if h.Method == "" {
		h.Method = "GET"
	}
	if h.Address == "" {
		h.Address = "http://localhost"
	}
	addr, err := url.Parse(h.Address)
	if err != nil {
		return err
	}
	if addr.Scheme != "http" && addr.Scheme != "https" {
		return errors.New("Only http and https are supported")
	}
	// Prepare data
	tags := map[string]string{"server": h.Address, "method": h.Method}
	var fields map[string]interface{}
	// Gather data
	fields, err = h.HTTPGather()
	if err != nil {
		return err
	}
	// Add metrics
	acc.AddFields("http_response", fields, tags)
	return nil
}

func loadConfig(configFile string, conf *Config) {
        source, err := ioutil.ReadFile(configFile)
        if err != nil {
                log.Fatalf("error: %v", err)
        }

        err = yaml.Unmarshal(source, conf)
        if err != nil {
                log.Fatalf("error: %v", err)
        }
}

func main() {
	conf := Config{}
	loadConfig("app.yml", &conf)

        acc := Accumulator{}
	for _, v := range conf.Addresses {
		h := new(HTTPResponse)
        	h.Address = v.Url
        	h.Method = v.Method
        	h.Gather(&acc)
	}
	acc.Print()
}
