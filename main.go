package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/slack-go/slack"
	"gopkg.in/yaml.v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	// Crowd Bank URL
	// status=21 募集開始前
	CrowdBankURL       = "https://crowdbank.jp"
	CrowdBankSearchURL = CrowdBankURL + "/api/v1/funds/search?keyword=&region=&project=&status=21"
)

type Config struct {
	UserAgent       string `yaml:"UserAgent"`
	Database        string `yaml:"Database"`
	SlackWebhookURL string `yaml:"SlackWebhookURL"`
}

type Fund struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	SubTitle    string `json:"subtitle"`
	LimitAmount int    `json:"limitAmount"`
	Rate        string `json:"rate"`
	Description string `json:"description"`
	Url         string `json:"url"`
	RegionName  string `json:"regionName"`
	ProjectName string `json:"projectName"`
	OpenTime    string `json:"openTime"`
	CloseTime   string `json:"closeTime"`
	LimitTime   string `json:"limitTime"`
	RaiseMethod string `json:"raiseMethod"`
	CurrencyID  string `json:"currencyId"`
}

type Data struct {
	Size  int    `json:"size"`
	Total int    `json:"total"`
	List  []Fund `json:"list"`
}

type Response struct {
	Data Data `json:"data"`
}

type SendList struct {
	gorm.Model

	FundID string `gorm:"unique"`
}

func Load(path string) (conf Config, err error) {
	fd, err := os.Open(path)
	if err != nil {
		return conf, err
	}
	buf, err := io.ReadAll(fd)
	if err != nil {
		return conf, err
	}
	err = yaml.Unmarshal(buf, &conf)
	if err != nil {
		return conf, err
	}

	return conf, nil
}

// CurrencyIDを文字列に変換
func (f *Fund) Currency() string {
	switch f.CurrencyID {
	case "1":
		return "日本円"
	case "2":
		return "USドル"
	case "3":
		return "AUドル"
	default:
		return "不明"
	}
}

func main() {
	var confPath string
	flag.StringVar(&confPath, "conf", "config.yaml", "Path to config file")
	flag.Parse()

	conf, err := Load(confPath)
	if err != nil {
		log.Fatalf("Failed to load config: %s", err)
	}

	// Database
	db, err := gorm.Open(sqlite.Open(conf.Database), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to open database: %s", err)
	}

	// Migrate
	db.AutoMigrate(&SendList{})

	// NewRequest
	req, err := http.NewRequest("GET", CrowdBankSearchURL, nil)
	if err != nil {
		log.Fatalf("Failed to create request: %s", err)
	}
	// User-Agentを設定
	req.Header.Set("User-Agent", conf.UserAgent)

	// Requset
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Failed to request: %s", err)
	}

	// json decode
	var response Response
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		log.Fatalf("Failed to decode json: %s", err)
	}

	for _, fund := range response.Data.List {
		// 既に送信済みの場合はスキップ
		var sendList SendList
		db.Where("fund_id = ?", fund.ID).First(&sendList)
		if sendList.FundID != "" {
			continue
		}

		// Slackに通知
		err := slack.PostWebhook(conf.SlackWebhookURL, &slack.WebhookMessage{
			Text: fund.Name,
			Attachments: []slack.Attachment{
				{
					Title:     fund.SubTitle,
					TitleLink: CrowdBankURL + fund.Url,
					Text:      fund.Description,
					Fields: []slack.AttachmentField{
						{
							Title: "地域",
							Value: fund.RegionName,
							Short: true,
						},
						{
							Title: "プロジェクト",
							Value: fund.ProjectName,
							Short: true,
						},
						{
							Title: "募集開始日",
							Value: fund.OpenTime,
							Short: true,
						},
						{
							Title: "募集終了日",
							Value: fund.CloseTime,
							Short: true,
						},
						{
							Title: "利率",
							Value: fund.Rate + "%",
							Short: true,
						},
						{
							Title: "募集方法",
							Value: fund.RaiseMethod,
							Short: true,
						},
						{
							Title: "通貨",
							Value: fund.Currency(),
							Short: true,
						},
					},
					MarkdownIn: []string{"text"},
				},
			},
		})
		if err != nil {
			log.Printf("Failed to post webhook: %s", err)
			continue
		}

		// DBに通知済みとして保存
		err = db.Create(&SendList{FundID: fund.ID}).Error
		if err != nil {
			log.Printf("Failed to save to database: %s", err)
		}
	}
}
