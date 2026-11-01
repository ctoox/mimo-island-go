package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	apiURL  = "https://platform.xiaomimimo.com/api/v1/tokenPlan/usage"
	userID  = "3210225705"
	appID   = "mimo-island"
)

type UsageData struct {
	Used    int     `json:"used"`
	Total   int     `json:"total"`
	Percent float64 `json:"percent"`
}

type APIResponse struct {
	Code int `json:"code"`
	Data struct {
		Usage struct {
			Items []struct {
				Name    string  `json:"name"`
				Used    int     `json:"used"`
				Limit   int     `json:"limit"`
				Percent float64 `json:"percent"`
			} `json:"items"`
		} `json:"usage"`
	} `json:"data"`
}

type Profile struct {
	Name      string `json:"name"`
	Cookie    string `json:"cookie"`
	LastUsed  string `json:"last_used,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

type Config struct {
	Profiles    []Profile `json:"profiles"`
	ActiveIndex int       `json:"active_index"`
}

type App struct {
	fyneApp    fyne.App
	window     fyne.Window
	config     Config
	configPath string
	usage      UsageData
	lastError  string
	mu         sync.RWMutex
	
	// UI elements
	statusLabel  *widget.Label
	usedLabel    *widget.Label
	totalLabel   *widget.Label
	progressBar  *widget.ProgressBar
	errorLabel   *widget.Label
	profileLabel *widget.Label
}

func main() {
	a := &App{}
	a.init()
	a.run()
}

func (a *App) init() {
	homeDir, _ := os.UserHomeDir()
	a.configPath = filepath.Join(homeDir, ".mimo_monitor", "profiles.json")
	a.loadConfig()
}

func (a *App) loadConfig() {
	dir := filepath.Dir(a.configPath)
	os.MkdirAll(dir, 0755)
	
	data, err := os.ReadFile(a.configPath)
	if err != nil {
		a.config = Config{}
		return
	}
	json.Unmarshal(data, &a.config)
}

func (a *App) saveConfig() {
	dir := filepath.Dir(a.configPath)
	os.MkdirAll(dir, 0755)
	
	data, _ := json.MarshalIndent(a.config, "", "  ")
	os.WriteFile(a.configPath, data, 0644)
}

func (a *App) getActiveProfile() *Profile {
	if len(a.config.Profiles) == 0 {
		return nil
	}
	idx := a.config.ActiveIndex
	if idx >= len(a.config.Profiles) {
		idx = 0
	}
	return &a.config.Profiles[idx]
}

func (a *App) fetchUsage() {
	profile := a.getActiveProfile()
	if profile == nil {
		a.mu.Lock()
		a.lastError = "请添加Cookie"
		a.mu.Unlock()
		return
	}
	
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("accept", "*/*")
	req.Header.Set("accept-language", "zh")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-timezone", "Asia/Shanghai")
	req.Header.Set("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("referer", "https://platform.xiaomimimo.com/console/plan-manage")
	req.Header.Set("cookie", profile.Cookie)
	
	resp, err := client.Do(req)
	if err != nil {
		a.mu.Lock()
		a.lastError = err.Error()[:30]
		profile.LastError = a.lastError
		a.mu.Unlock()
		a.saveConfig()
		return
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == 401 {
		a.mu.Lock()
		a.lastError = "Cookie已过期"
		profile.LastError = "401"
		a.mu.Unlock()
		a.saveConfig()
		return
	}
	
	if resp.StatusCode != 200 {
		a.mu.Lock()
		a.lastError = fmt.Sprintf("HTTP %d", resp.StatusCode)
		profile.LastError = a.lastError
		a.mu.Unlock()
		a.saveConfig()
		return
	}
	
	var apiResp APIResponse
	json.NewDecoder(resp.Body).Decode(&apiResp)
	
	for _, item := range apiResp.Data.Usage.Items {
		if item.Name == "plan_total_token" {
			a.mu.Lock()
			a.usage = UsageData{
				Used:    item.Used,
				Total:   item.Limit,
				Percent: item.Percent * 100,
			}
			a.lastError = ""
			profile.LastError = ""
			profile.LastUsed = time.Now().Format(time.RFC3339)
			a.mu.Unlock()
			a.saveConfig()
			return
		}
	}
}

func (a *App) updateUI() {
	a.mu.RLock()
	defer a.mu.RUnlock()
	
	profile := a.getActiveProfile()
	if profile != nil {
		a.profileLabel.SetText(profile.Name)
	}
	
	if a.lastError != "" {
		a.errorLabel.SetText(a.lastError)
		a.errorLabel.Show()
		a.usedLabel.Hide()
		a.totalLabel.Hide()
		a.progressBar.Hide()
	} else {
		a.errorLabel.Hide()
		a.usedLabel.Show()
		a.totalLabel.Show()
		a.progressBar.Show()
		
		a.usedLabel.SetText(fmt.Sprintf("已用: %d", a.usage.Used))
		a.totalLabel.SetText(fmt.Sprintf("总额: %d", a.usage.Total))
		a.progressBar.SetValue(a.usage.Percent / 100)
		a.statusLabel.SetText(fmt.Sprintf("%.1f%%", a.usage.Percent))
	}
}

func (a *App) addCookie() {
	entry := widget.NewMultiLineEntry()
	entry.SetPlaceHolder("粘贴 Cookie (fetch/curl/纯字符串)")
	
	dialog := widget.NewModalPopUp(
		container.NewVBox(
			widget.NewLabel("添加 Cookie"),
			entry,
			container.NewHBox(
				widget.NewButton("取消", func() {
					dialog.Hide()
				}),
				widget.NewButton("确定", func() {
					cookie := entry.Text
					if cookie == "" {
						return
					}
					
					nameEntry := widget.NewEntry()
					nameEntry.SetPlaceHolder("配置名称")
					nameEntry.SetText(fmt.Sprintf("账号%d", len(a.config.Profiles)+1))
					
					nameDialog := widget.NewModalPopUp(
						container.NewVBox(
							widget.NewLabel("命名配置"),
							nameEntry,
							container.NewHBox(
								widget.NewButton("取消", func() {
									nameDialog.Hide()
								}),
								widget.NewButton("确定", func() {
									if nameEntry.Text != "" {
										a.config.Profiles = append(a.config.Profiles, Profile{
											Name:   nameEntry.Text,
											Cookie: cookie,
										})
										a.config.ActiveIndex = len(a.config.Profiles) - 1
										a.saveConfig()
										a.fetchUsage()
										a.updateUI()
									}
									nameDialog.Hide()
								}),
							),
						),
						a.window.Canvas(),
					)
					nameDialog.Show()
					dialog.Hide()
				}),
			),
		),
		a.window.Canvas(),
	)
	dialog.Show()
}

func (a *App) run() {
	a.fyneApp = app.NewWithID(appID)
	a.window = a.fyneApp.NewWindow("MiMo Island")
	a.window.SetFixedSize(true)
	a.window.Resize(fyne.NewSize(320, 200))
	
	// Create UI elements
	a.statusLabel = widget.NewLabel("0%")
	a.statusLabel.TextStyle = fyne.TextStyle{Bold: true}
	
	a.usedLabel = widget.NewLabel("已用: 0")
	a.totalLabel = widget.NewLabel("总额: 0")
	
	a.progressBar = widget.NewProgressBar()
	
	a.errorLabel = widget.NewLabel("")
	a.errorLabel.Hide()
	
	a.profileLabel = widget.NewLabel("未配置")
	
	// Layout
	title := canvas.NewText("MiMo Token Monitor", color.NRGBA{R: 235, G: 237, B: 245, A: 255})
	title.TextSize = 16
	title.TextStyle = fyne.TextStyle{Bold: true}
	
	content := container.NewVBox(
		container.NewHBox(title),
		widget.NewSeparator(),
		a.profileLabel,
		container.NewGridWithColumns(2,
			a.usedLabel,
			a.totalLabel,
		),
		a.progressBar,
		a.errorLabel,
		container.NewHBox(
			widget.NewButton("添加Cookie", func() {
				a.addCookie()
			}),
			widget.NewButton("刷新", func() {
				go func() {
					a.fetchUsage()
					a.updateUI()
				}()
			}),
		),
	)
	
	a.window.SetContent(content)
	
	// Initial fetch
	go func() {
		a.fetchUsage()
		a.updateUI()
	}()
	
	// Auto refresh every 5 minutes
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			a.fetchUsage()
			a.updateUI()
		}
	}()
	
	a.window.ShowAndRun()
}
