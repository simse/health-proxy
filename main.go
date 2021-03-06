package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
)

type WithingsRefreshResponse struct {
	Status int               `json:"status"`
	Body   map[string]string `json:"body"`
}

func getRefreshToken() string {
	fileContents, _ := ioutil.ReadFile("/data/REFRESH_TOKEN")

	return strings.TrimSuffix(string(fileContents), "\n")
}

func setRefreshToken(input string) {
	ioutil.WriteFile("/data/REFRESH_TOKEN", []byte(input), 0644)
}

func cycleRefreshToken() string {
	client := resty.New()

	resp, _ := client.R().
		SetFormData(map[string]string{
			"action":        "requesttoken",
			"client_id":     os.Getenv("WITHINGS_CLIENT_ID"),
			"client_secret": os.Getenv("WITHINGS_CLIENT_SECRET"),
			"grant_type":    "refresh_token",
			"refresh_token": getRefreshToken(),
		}).
		Post("https://wbsapi.withings.net/v2/oauth2")

	parsedResponse := WithingsRefreshResponse{}

	if resp.Body() != nil {
		json.Unmarshal(resp.Body(), &parsedResponse)

		if parsedResponse.Status != 0 {
			fmt.Println("ERROR unable to refresh Withings token")
			fmt.Println(parsedResponse.Body)
			return ""
		}

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

var previousWeightRecordings int = 0

func getWeightData(accessToken string) WithingsDataResponse {
	client := resty.New()

	resp, _ := client.R().
		SetFormData(map[string]string{
			"action":    "getmeas",
			"meastypes": "1,6,76,77,88",
			"startdate": "11-09-21",
		}).
		SetHeader("Authorization", "Bearer "+accessToken).
		Post("https://wbsapi.withings.net/measure")

	parsedResponse := WithingsDataResponse{}

	if resp.Body() != nil {
		json.Unmarshal(resp.Body(), &parsedResponse)

		if parsedResponse.Status != 0 {
			fmt.Println("[ERROR] unable to fetch Withings weight data")
			return WithingsDataResponse{
				Status: parsedResponse.Status,
			}
		}
	}

	// Detect change
	if previousWeightRecordings != 0 && previousWeightRecordings != len(parsedResponse.Body.MeasureGroups) {
		fmt.Println("weight data CHANGE DETECTED")
		client.R().Post("https://webhook.gatsbyjs.com/hooks/data_source/publish/b2f6b3fe-6899-4e88-b121-fe678b6dbd98")
	} else {
		previousWeightRecordings = len(parsedResponse.Body.MeasureGroups)
	}

	return parsedResponse
}

type Weight struct {
	Current       WeightHistoryPoint   `json:"current"`
	WeightHistory []WeightHistoryPoint `json:"history"`
}

type WeightHistoryPoint struct {
	Weight        float64   `json:"weight"`
	FatPercentage float64   `json:"fat_percentage"`
	MuscleMass    float64   `json:"muscle_mass"`
	BoneMass      float64   `json:"bone_mass"`
	Hydration     float64   `json:"hydration"`
	BodyMassIndex float64   `json:"bmi"`
	Date          time.Time `json:"date"`
}

var weight Weight
var accessToken string

func updateWeightStats() {
	weightData := getWeightData(accessToken)

	measurementPoints := []WeightHistoryPoint{}

	if weightData.Status != 0 {
		return
	}

	for _, x := range weightData.Body.MeasureGroups {
		if x.Attribute == 0 {
			point := WeightHistoryPoint{
				Date: time.Unix(int64(x.Date), 0),
			}

			for _, m := range x.Measures {
				if m.Type == 1 {
					point.Weight = m.GetValue()
				}

				if m.Type == 6 {
					point.FatPercentage = m.GetValue()
				}

				if m.Type == 76 {
					point.MuscleMass = m.GetValue()
				}

				if m.Type == 77 {
					point.Hydration = m.GetValue()
				}

				if m.Type == 88 {
					point.BoneMass = m.GetValue()
				}
			}

			// Skip entire measurement group if there is an empty value
			if point.Weight == 0 || point.FatPercentage == 0 {
				continue
			}

			// Calculate BMI
			point.BodyMassIndex = point.Weight / float64(172) / float64(172) * float64(10000)

			measurementPoints = append(measurementPoints, point)
		}
	}

	sort.Slice(measurementPoints, func(i, j int) bool {
		return measurementPoints[i].Date.Before(measurementPoints[j].Date)
	})

	weight.WeightHistory = measurementPoints
	weight.Current = measurementPoints[len(measurementPoints)-1]

	fmt.Println("Updated weight stats")
}

func updateAccessToken() {
	accessToken = cycleRefreshToken()

	fmt.Println("Updated Withings access token")
}

func main() {
	gin.SetMode(gin.ReleaseMode)

	godotenv.Load()

	r := gin.Default()
	r.Use(cors.Default())
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})

	r.GET("/weight", func(c *gin.Context) {
		c.JSON(200, weight)
	})

	r.GET("/v1/summary", func(c *gin.Context) {
		c.JSON(200, map[string]interface{}{
			"weight": weight,
		})
	})

	u, _ := url.Parse("https://account.withings.com/oauth2_user/authorize2")

	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", os.Getenv("WITHINGS_CLIENT_ID"))
	q.Set("state", "unspoofed")
	q.Set("scope", "user.metrics")
	q.Set("redirect_uri", "https://api.health.simse.io/withings-callback")
	u.RawQuery = q.Encode()

	fmt.Println(u.String())

	// Authentication flow
	r.GET("/withings/authenticate", func(c *gin.Context) {

		c.Redirect(301, u.String())
	})

	r.GET("/withings-callback", func(c *gin.Context) {
		fmt.Println(c.Params)
		client := resty.New()

		code := c.Query("code")
		fmt.Println(code)

		resp, _ := client.R().
			SetFormData(map[string]string{
				"action":        "requesttoken",
				"client_id":     os.Getenv("WITHINGS_CLIENT_ID"),
				"client_secret": os.Getenv("WITHINGS_CLIENT_SECRET"),
				"grant_type":    "authorization_code",
				"code":          code,
				"redirect_uri":  "https://api.health.simse.io/withings-callback",
			}).
			Post("https://wbsapi.withings.net/v2/oauth2")

		parsedResponse := WithingsRefreshResponse{}

		if resp.Body() != nil {
			json.Unmarshal(resp.Body(), &parsedResponse)

			fmt.Println(string(resp.Body()))
			fmt.Println(parsedResponse)

			setRefreshToken(parsedResponse.Body["refresh_token"])
		}

		c.String(200, "OK")
	})

	// fmt.Println(getRefreshToken())

	// Start up procedure
	updateAccessToken()
	updateWeightStats()

	// Set up cron
	c := cron.New()
	c.AddFunc("@hourly", func() { updateAccessToken() })
	c.AddFunc("* * * * *", func() { updateWeightStats() })
	c.Start()

	r.Run(":5000")
}
