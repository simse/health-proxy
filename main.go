package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"github.com/robfig/cron/v3"
)

type WithingsRefreshResponse struct {
	Status int               `json:"status"`
	Body   map[string]string `json:"body"`
}

func getRefreshToken() string {
	fileContents, _ := ioutil.ReadFile("./REFRESH_TOKEN")

	return string(fileContents)
}

func setRefreshToken(input string) {
	ioutil.WriteFile("./REFRESH_TOKEN", []byte(input), 0644)
}

func cycleRefreshToken() string {
	client := resty.New()

	resp, _ := client.R().
		SetFormData(map[string]string{
			"action":        "requesttoken",
			"client_id":     "e04286867ca5236b4b398800fcc005a78cd4ad803d301108a8b0dd8612217938",
			"client_secret": "de1adf737d3528765c620e9d3fe4f89999289745ff283ecd6f28bd05585f3a4d",
			"grant_type":    "refresh_token",
			"refresh_token": getRefreshToken(),
		}).
		Post("https://wbsapi.withings.net/v2/oauth2")

	parsedResponse := WithingsRefreshResponse{}

	if resp.Body() != nil {
		json.Unmarshal(resp.Body(), &parsedResponse)

		fmt.Println(string(resp.Body()))
		fmt.Println(parsedResponse)

		setRefreshToken(parsedResponse.Body["refresh_token"])
	}

	// fmt.Println(resp)

	return parsedResponse.Body["access_token"]
}

type WithingsDataResponse struct {
	Status int                      `json:"status"`
	Body   WithingsDataResponseBody `json:"body"`
}

type WithingsDataResponseBody struct {
	UpdateTime    string              `json:"updatetime"`
	TimeZone      string              `json:"timezone"`
	More          int                 `json:"more"`
	Offset        int                 `json:"offset"`
	MeasureGroups []WithingsDataGroup `json:"measuregrps"`
}

type WithingsDataGroup struct {
	Date      int                        `json:"date"`
	Attribute int                        `json:"attrib"`
	Measures  []WithingsDataGroupMeasure `json:"measures"`
}

type WithingsDataGroupMeasure struct {
	Value int `json:"value"`
	Type  int `json:"type"`
	Unit  int `json:"unit"`
}

func (m *WithingsDataGroupMeasure) GetValue() float64 {
	return float64(m.Value) * math.Pow10(m.Unit)
}

func getWeightData(accessToken string) WithingsDataResponse {
	client := resty.New()

	resp, _ := client.R().
		SetFormData(map[string]string{
			"action":    "getmeas",
			"meastypes": "1,6",
			"startdate": "11-09-21",
		}).
		SetHeader("Authorization", "Bearer "+accessToken).
		Post("https://wbsapi.withings.net/measure")

	parsedResponse := WithingsDataResponse{}

	if resp.Body() != nil {
		json.Unmarshal(resp.Body(), &parsedResponse)
	}

	return parsedResponse
}

type Weight struct {
	CurrentWeight float64              `json:"current_weight"`
	WeightHistory []WeightHistoryPoint `json:"weight_history"`
}

type WeightHistoryPoint struct {
	Weight float64 `json:"weight"`
	Date   int     `json:"date"`
}

var weight Weight
var accessToken string

func updateWeightStats() {
	weightData := getWeightData(accessToken)

	measurementPoints := []WeightHistoryPoint{}

	for _, x := range weightData.Body.MeasureGroups {
		if x.Attribute == 0 {
			for _, m := range x.Measures {
				if m.Type == 1 {
					measurementPoints = append(measurementPoints, WeightHistoryPoint{
						Weight: m.GetValue(),
						Date:   x.Date,
					})
				}
			}
		}
	}

	weight.WeightHistory = measurementPoints
	weight.CurrentWeight = measurementPoints[len(measurementPoints)-1].Weight

	fmt.Println("Updated weight stats")
}

func updateAccessToken() {
	accessToken = cycleRefreshToken()

	fmt.Println("Updated Withings access token")
}

func main() {
	gin.SetMode(gin.ReleaseMode)

	r := gin.Default()
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})

	r.GET("/weight", func(c *gin.Context) {
		c.JSON(200, weight)
	})

	fmt.Println(getRefreshToken())

	// Start up procedure
	updateAccessToken()
	updateWeightStats()

	// Set up cron
	c := cron.New()
	c.AddFunc("@hourly", func() { updateAccessToken() })
	c.AddFunc("* * * * *", func() { updateWeightStats() })
	c.Start()

	r.Run()
}
