package headless

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"github.com/rs/zerolog"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/config"
	"github.com/colzphml/mega_games/discord_kafka_game_image/internal/types"
)

// log is the package-level logger configured for structured logging.
var log = zerolog.New(os.Stdout).With().Str("package", "headless").Timestamp().Logger()

const defaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130 Safari/537.36"

// Client represents a headless client that renders recap images from the API.
type Client struct {
	baseURL     string
	league      string
	httpTimeout time.Duration
}

// NewClient initializes a new headless client with the provided configuration.
func NewClient(ctx context.Context, cfg config.Config) (*Client, error) {
	return &Client{
		baseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		league:      strings.TrimSpace(cfg.League),
		httpTimeout: cfg.FetchTimeout,
	}, nil
}

// Close performs any necessary cleanup before the client is disposed of.
func (c *Client) Close() error {
	log.Info().Msg("closing headless client")
	return nil
}

func (c *Client) Fetch(ctx context.Context, gameID string) (types.Result, error) {
	gameURL, err := buildGameURL(c.baseURL, c.league, gameID)
	if err != nil {
		return types.Result{}, err
	}
	recapURL, err := recapURLFromGameURL(gameURL)
	if err != nil {
		return types.Result{}, err
	}
	recData, err := fetchJSON(ctx, recapURL, c.httpTimeout)
	if err != nil {
		return types.Result{}, err
	}
	img, err := buildRecapImage(ctx, c.baseURL, c.httpTimeout, recData)
	if err != nil {
		return types.Result{}, err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return types.Result{}, fmt.Errorf("encode recap image: %w", err)
	}

	return types.Result{
		GameURL:     gameURL,
		ContentType: "image/png",
		Image:       buf.Bytes(),
	}, nil
}

func buildGameURL(baseURL, league, gameID string) (string, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	league = strings.TrimSpace(league)
	gameID = strings.TrimSpace(gameID)
	if baseURL == "" {
		return "", fmt.Errorf("empty base url")
	}
	if league == "" || gameID == "" {
		return "", fmt.Errorf("missing league or game id")
	}
	return fmt.Sprintf("%s/leagues/%s/games/%s", baseURL, league, gameID), nil
}

func recapURLFromGameURL(gameURL string) (string, error) {
	trimmed := strings.TrimSpace(gameURL)
	if trimmed == "" {
		return "", fmt.Errorf("empty game url")
	}
	if idx := strings.Index(trimmed, "?"); idx != -1 {
		trimmed = trimmed[:idx]
	}
	trimmed = strings.TrimRight(trimmed, "/")
	if !strings.Contains(trimmed, "/leagues/") {
		return "", fmt.Errorf("unexpected game url: %s", gameURL)
	}
	recapURL := strings.Replace(trimmed, "/leagues/", "/api/leagues/", 1)
	return recapURL + "/recap/", nil
}

// Структуры для парсинга JSON (не все поля)
type Recap struct {
	Game struct {
		HomeTeam  Team `json:"homeTeam"`
		AwayTeam  Team `json:"awayTeam"`
		HomeScore int  `json:"homeScore"`
		AwayScore int  `json:"awayScore"`
		WeekIndex int  `json:"weekIndex"`
	} `json:"game"`
	PassHomeStats *Stat `json:"pass_home_stats"`
	PassAwayStats *Stat `json:"pass_away_stats"`
	RushHomeStats *Stat `json:"rush_home_stats"`
	RushAwayStats *Stat `json:"rush_away_stats"`
	RecHomeStats  *Stat `json:"rec_home_stats"`
	RecAwayStats  *Stat `json:"rec_away_stats"`
	DefHomeStats  *Stat `json:"def_home_stats"`
	DefAwayStats  *Stat `json:"def_away_stats"`
}

type Team struct {
	DisplayName  string  `json:"displayName"`
	CityName     string  `json:"cityName"`
	TotalWins    int     `json:"totalWins"`
	TotalLosses  int     `json:"totalLosses"`
	TotalTies    int     `json:"totalTies"`
	LogoID       int     `json:"logoId"`
	PrimaryColor string  `json:"primaryColor"`
	Logo         *string `json:"logo"`
}

type Stat struct {
	Player struct {
		FullName string `json:"fullName"`
	} `json:"player"`
	PassComp        JSONInt `json:"passComp"`
	PassAtt         JSONInt `json:"passAtt"`
	PassYds         JSONInt `json:"passYds"`
	PassTDs         JSONInt `json:"passTDs"`
	PassInts        JSONInt `json:"passInts"`
	RushAtt         JSONInt `json:"rushAtt"`
	RushYds         JSONInt `json:"rushYds"`
	RushTDs         JSONInt `json:"rushTDs"`
	RecCatches      JSONInt `json:"recCatches"`
	RecYds          JSONInt `json:"recYds"`
	RecTDs          JSONInt `json:"recTDs"`
	DefTotalTackles JSONInt `json:"defTotalTackles"`
	DefInts         JSONInt `json:"defInts"`
	DefSacks        JSONInt `json:"defSacks"`
	DefDeflections  JSONInt `json:"defDeflections"`
	DefForcedFum    JSONInt `json:"defForcedFum"`
	DefFumRec       JSONInt `json:"defFumRec"`
	DefTDs          JSONInt `json:"defTDs"`
}

// JSONInt supports numbers encoded as JSON numbers or strings.
type JSONInt int

func (i *JSONInt) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*i = 0
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			*i = 0
			return nil
		}
		n, err := parseJSONIntString(s)
		if err != nil {
			return err
		}
		*i = JSONInt(n)
		return nil
	}
	var n float64
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*i = JSONInt(int(math.Round(n)))
	return nil
}

func parseJSONIntString(s string) (int, error) {
	if strings.Contains(s, ".") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, err
		}
		return int(math.Round(f)), nil
	}
	return strconv.Atoi(s)
}

func buildRecapImage(ctx context.Context, baseURL string, httpTimeout time.Duration, recData Recap) (image.Image, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	// Загружаем фон стадиона и логотипы
	bg, err := loadImage(ctx, baseURL+"/images/stadiums/"+strconv.Itoa(recData.Game.HomeTeam.LogoID)+".png", httpTimeout)
	if err != nil {
		return nil, err
	}
	logoHome, err := loadLogo(ctx, baseURL, recData.Game.HomeTeam, httpTimeout)
	if err != nil {
		return nil, err
	}
	logoAway, err := loadLogo(ctx, baseURL, recData.Game.AwayTeam, httpTimeout)
	if err != nil {
		return nil, err
	}
	footerLogo, err := loadImage(ctx, baseURL+"/logo.png", httpTimeout)
	if err != nil {
		return nil, err
	}

	// Настройка холста
	const width, height = 2048, 1152
	dc := gg.NewContext(width, height)

	// 1. фон стадиона
	dc.DrawImage(bg, 0, 0)

	// 2. горизонтальный затемняющий градиент
	drawSideGradient(dc, width, height)

	// 3. внутренний контейнер
	cx, cy := 256.0, 64.0
	cw, ch := 1536.0, 864.0
	drawContainer(dc, cx, cy, cw, ch)

	// 4. верхняя полоска с заголовком
	drawTopBar(dc, recData, cx, cy, cw)

	// 5. строки команд
	drawTeamRows(dc, recData, cx, cy+128, cw, logoAway, logoHome)

	// 6. статистика
	drawStatsSection(dc, recData, cx, cy+128+2*152, cw)

	// 7. футер
	drawFooter(dc, cx, cy+864-68, cw, footerLogo)

	return dc.Image(), nil
}

// fetchJSON скачивает и распарсивает JSON
func fetchJSON(ctx context.Context, url string, timeout time.Duration) (Recap, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Recap{}, fmt.Errorf("fetch recap json: %w", err)
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: timeout}
	res, err := client.Do(req)
	if err != nil {
		return Recap{}, fmt.Errorf("fetch recap json: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Recap{}, fmt.Errorf("fetch recap json: %s", res.Status)
	}
	var rec Recap
	if err := json.NewDecoder(res.Body).Decode(&rec); err != nil {
		return Recap{}, fmt.Errorf("decode recap json: %w", err)
	}
	return rec, nil
}

// loadImage скачивает изображение и декодирует его
func loadImage(ctx context.Context, url string, timeout time.Duration) (image.Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch image: %w", err)
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "image/*,*/*;q=0.8")
	client := &http.Client{Timeout: timeout}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch image: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch image: %s", res.Status)
	}
	img, _, err := image.Decode(res.Body)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	return img, nil
}

// loadLogo выбирает адрес логотипа (пользовательский или из каталога) и загружает его
func loadLogo(ctx context.Context, baseURL string, t Team, timeout time.Duration) (image.Image, error) {
	if t.Logo != nil {
		return loadImage(ctx, *t.Logo, timeout)
	}
	return loadImage(ctx, strings.TrimRight(baseURL, "/")+"/images/teamlogos/256/"+strconv.Itoa(t.LogoID)+".png", timeout)
}

// drawSideGradient затемняет края изображения
func drawSideGradient(dc *gg.Context, w, h int) {
	grad := gg.NewLinearGradient(0, 0, float64(w), 0)
	// цвет #111 с альфой 0.7
	grad.AddColorStop(0, color.RGBA{17, 17, 17, 178})
	grad.AddColorStop(0.5, color.RGBA{17, 17, 17, 0})
	grad.AddColorStop(1, color.RGBA{17, 17, 17, 178})
	dc.SetFillStyle(grad)
	dc.DrawRectangle(0, 0, float64(w), float64(h))
	dc.Fill()
}

// drawContainer рисует рамку внутреннего контейнера
func drawContainer(dc *gg.Context, x, y, w, h float64) {
	dc.SetLineWidth(2)
	dc.SetColor(color.RGBA{0xc9, 0xcc, 0xcc, 0xff})
	dc.DrawRectangle(x, y, w, h)
	dc.Stroke()
}

// drawTopBar рисует верхнюю панель с заголовком и отметкой "Final"
func drawTopBar(dc *gg.Context, rec Recap, x, y, w float64) {
	barH := 128.0
	// фоновой вертикальный градиент
	grad := gg.NewLinearGradient(x, y, x, y+barH)
	grad.AddColorStop(0, color.RGBA{0x25, 0x39, 0x4d, 255})
	grad.AddColorStop(1, color.RGBA{0x0e, 0x0f, 0x17, 255})
	dc.SetFillStyle(grad)
	dc.DrawRectangle(x, y, w, barH)
	dc.Fill()
	// нижняя линия
	dc.SetColor(color.RGBA{0xc9, 0xcc, 0xcc, 255})
	dc.DrawLine(x, y+barH, x+w, y+barH)
	dc.Stroke()

	// текст – название лиги и неделя (пример)
	title := "MEGA - Неделя " + strconv.Itoa(rec.Game.WeekIndex+1)
	// загружаем шрифт Roboto
	face := loadFont(48)
	dc.SetFontFace(face)
	dc.SetColor(color.White)
	dc.DrawStringAnchored(title, x+32, y+barH/2, 0, 0.5)

	// плашка "FINAL"
	hiW := w * 0.2
	dc.SetColor(color.RGBA{0xfe, 0xb9, 0x1f, 255})
	dc.DrawRectangle(x+w-hiW, y, hiW, barH)
	dc.Fill()
	dc.SetColor(color.RGBA{0x25, 0x39, 0x4d, 255})
	face2 := loadFont(35)
	dc.SetFontFace(face2)
	dc.DrawStringAnchored("FINAL", x+w-hiW/2, y+barH/2, 0.5, 0.5)
}

// drawTeamRows рисует две строки с командами
func drawTeamRows(dc *gg.Context, rec Recap, x, y, w float64, logoAway, logoHome image.Image) {
	rowH := 152.0
	// Верхняя (гостевая) команда
	drawTeamRow(dc, rec.Game.AwayTeam, rec.Game.AwayScore, x, y, w, rowH, logoAway)
	// Нижняя (домашняя) команда
	drawTeamRow(dc, rec.Game.HomeTeam, rec.Game.HomeScore, x, y+rowH, w, rowH, logoHome)
}

func drawTeamRow(dc *gg.Context, t Team, score int, x, y, w, h float64, logo image.Image) {
	// разделяем ширину: 20 %, 60 %, 20 %
	colLogoW := w * 0.2
	colNameW := w * 0.6
	colScoreW := w * 0.2

	// цвета команды
	col := parseColor(t.PrimaryColor)

	// 1. фон логотипа
	dc.SetColor(col)
	dc.DrawRectangle(x, y, colLogoW, h)
	dc.Fill()
	// логотип
	dc.DrawImageAnchored(logo, int(x+colLogoW/2), int(y+h/2), 0.5, 0.5)

	// 2. фон имени
	dc.SetColor(col)
	dc.DrawRectangle(x+colLogoW, y, colNameW, h)
	dc.Fill()
	// затемняющая полоса
	dc.SetColor(color.RGBA{0, 0, 0, 0x26})
	dc.DrawRectangle(x+colLogoW, y, colNameW, h)
	dc.Fill()
	// текст: город, маскот, рекорд
	dc.SetColor(color.White)
	faceLoc := loadFont(25)
	faceMascot := loadFont(77)
	faceRecord := loadFont(22)
	dc.SetFontFace(faceLoc)
	dc.DrawStringAnchored(t.CityName, x+colLogoW+32, y+40, 0, 0)
	dc.SetFontFace(faceMascot)
	dc.DrawStringAnchored(t.DisplayName, x+colLogoW+32, y+95, 0, 0)
	recStr := strconv.Itoa(t.TotalWins) + "-" + strconv.Itoa(t.TotalLosses) + "-" + strconv.Itoa(t.TotalTies)
	dc.SetFontFace(faceRecord)
	dc.DrawStringAnchored(recStr, x+colLogoW+32, y+130, 0, 0)

	// 3. фон счёта
	dc.SetColor(col)
	dc.DrawRectangle(x+colLogoW+colNameW, y, colScoreW, h)
	dc.Fill()
	// счёт
	dc.SetColor(color.White)
	faceScore := loadFont(102)
	dc.SetFontFace(faceScore)
	dc.DrawStringAnchored(strconv.Itoa(score), x+colLogoW+colNameW+colScoreW/2, y+h/2+10, 0.5, 0.5)

	// линия снизу
	dc.SetColor(color.RGBA{0xc9, 0xcc, 0xcc, 0xff})
	dc.DrawLine(x, y+h, x+w, y+h)
	dc.Stroke()
}

// drawStatsSection рисует блок статистики (4 строки, два столбца)
func drawStatsSection(dc *gg.Context, rec Recap, x, y, w float64) {
	h := 361.6
	dc.SetColor(color.RGBA{0xc9, 0xcc, 0xcc, 0xff})
	dc.DrawRectangle(x, y, w, h)
	dc.Fill()

	// Разделяем пополам на home/away
	colW := w / 2
	drawStatsSide(dc, rec.Game.AwayTeam, rec.PassAwayStats, rec.RushAwayStats, rec.RecAwayStats, rec.DefAwayStats, x, y, colW, h, true)
	drawStatsSide(dc, rec.Game.HomeTeam, rec.PassHomeStats, rec.RushHomeStats, rec.RecHomeStats, rec.DefHomeStats, x+colW, y, colW, h, false)
}

// drawStatsSide рисует одну половину статистики
func drawStatsSide(dc *gg.Context, team Team, pass, rush, recStat, def *Stat, x, y, w, h float64, left bool) {
	// бар с названием команды
	barW := w * 0.075
	dc.SetColor(parseColor(team.PrimaryColor))
	dc.DrawRectangle(x, y, barW, h)
	dc.Fill()
	// вертикальное название
	face := loadFont(28)
	dc.SetFontFace(face)
	dc.SetColor(color.White)
	// поворачиваем контекст
	dc.Push()
	if left {
		// поворот против часовой на 90°
		dc.RotateAbout(-gg.Radians(90), x+barW/2, y+h/2)
		dc.DrawStringAnchored(team.DisplayName, x+barW/2, y+h/2, 0.5, 0.5)
	} else {
		dc.RotateAbout(gg.Radians(90), x+barW/2, y+h/2)
		dc.DrawStringAnchored(team.DisplayName, x+barW/2, y+h/2, 0.5, 0.5)
	}
	dc.Pop()

	// область статистики
	statX := x + barW
	statW := w - barW
	rowH := h / 4
	// массив категорий и соответствующих данных
	rows := []struct {
		label string
		stat  *Stat
	}{
		{"Pass", pass},
		{"Rush", rush},
		{"Rec", recStat},
		{"Def", def},
	}
	for i, row := range rows {
		ry := y + float64(i)*rowH
		// разделительные линии
		if i > 0 {
			dc.SetColor(color.RGBA{0xba, 0xba, 0xba, 0xff})
			dc.DrawLine(statX, ry, statX+statW, ry)
			dc.Stroke()
		}
		// надпись игрока
		if row.stat != nil {
			dc.SetColor(color.RGBA{0x25, 0x39, 0x4d, 0xff})
			dc.SetFontFace(loadFont(32))
			dc.DrawStringAnchored(row.stat.Player.FullName, statX+16, ry+28, 0, 0)
			// строка статистики
			dc.SetFontFace(loadFont(28))
			dc.DrawStringAnchored(formatStat(row.label, row.stat), statX+16, ry+63, 0, 0)
		}
	}
}

// форматирование строк статистики
func formatStat(cat string, s *Stat) string {
	if s == nil {
		return ""
	}
	switch cat {
	case "Pass":
		str := strconv.Itoa(int(s.PassComp)) + "/" + strconv.Itoa(int(s.PassAtt)) + ", " + strconv.Itoa(int(s.PassYds)) + " YDS"
		if s.PassTDs > 0 {
			str += ", " + strconv.Itoa(int(s.PassTDs)) + " TD"
		}
		if s.PassInts > 0 {
			str += ", " + strconv.Itoa(int(s.PassInts)) + " INT"
		}
		return str
	case "Rush":
		str := strconv.Itoa(int(s.RushAtt)) + " CARRIES, " + strconv.Itoa(int(s.RushYds)) + " YDS"
		if s.RushTDs > 0 {
			str += ", " + strconv.Itoa(int(s.RushTDs)) + " TD"
		}
		return str
	case "Rec":
		str := strconv.Itoa(int(s.RecCatches)) + " REC"
		if s.RecYds > 0 {
			str += ", " + strconv.Itoa(int(s.RecYds)) + " YDS"
		}
		if s.RecTDs > 0 {
			str += ", " + strconv.Itoa(int(s.RecTDs)) + " TD"
		}
		return str
	case "Def":
		str := strconv.Itoa(int(s.DefTotalTackles)) + " TKL"
		if s.DefInts > 0 {
			str += ", " + strconv.Itoa(int(s.DefInts)) + " INT"
		}
		if s.DefSacks > 0 {
			str += ", " + strconv.Itoa(int(s.DefSacks)) + " SACK"
		}
		if s.DefDeflections > 0 {
			str += ", " + strconv.Itoa(int(s.DefDeflections)) + " DFL"
		}
		if s.DefForcedFum > 0 {
			str += ", " + strconv.Itoa(int(s.DefForcedFum)) + " FF"
		}
		if s.DefFumRec > 0 {
			str += ", " + strconv.Itoa(int(s.DefFumRec)) + " FR"
		}
		if s.DefTDs > 0 {
			str += ", " + strconv.Itoa(int(s.DefTDs)) + " TD"
		}
		return str
	}
	return ""
}

// drawFooter рисует нижнюю полосу
func drawFooter(dc *gg.Context, x, y, w float64, logo image.Image) {
	h := 68.0
	// градиент
	grad := gg.NewLinearGradient(x, y, x, y+h)
	grad.AddColorStop(0, color.RGBA{0x25, 0x39, 0x4d, 255})
	grad.AddColorStop(1, color.RGBA{0x0e, 0x0f, 0x17, 255})
	dc.SetFillStyle(grad)
	dc.DrawRectangle(x, y, w, h)
	dc.Fill()
	// верхняя линия
	dc.SetColor(color.RGBA{0xc9, 0xcc, 0xcc, 255})
	dc.DrawLine(x, y, x+w, y)
	dc.Stroke()
	// текст
	dc.SetColor(color.White)
	dc.SetFontFace(loadFont(32))
	dc.DrawStringAnchored("NEONSPORTZ.COM", x+16, y+h/2, 0, 0.5)
	// логотип справа
	if logo != nil {
		logoH := h
		ratio := float64(logo.Bounds().Dx()) / float64(logo.Bounds().Dy())
		logoW := logoH * ratio
		dc.DrawImageAnchored(logo, int(x+w-logoW/2-16), int(y+h/2), 0.5, 0.5)
	}
}

// parseColor преобразует десятичную строку цвета в цвет RGBA
func parseColor(dec string) color.RGBA {
	// decimal string → int
	i, _ := strconv.ParseInt(dec, 10, 64)
	hex := strconv.FormatInt(i, 16)
	// заполняем ведущие нули
	for len(hex) < 6 {
		hex = "0" + hex
	}
	// разбиваем
	r, _ := strconv.ParseInt(hex[0:2], 16, 64)
	g, _ := strconv.ParseInt(hex[2:4], 16, 64)
	b, _ := strconv.ParseInt(hex[4:6], 16, 64)
	return color.RGBA{uint8(r), uint8(g), uint8(b), 255}
}

// загрузка шрифта Roboto
func loadFont(size float64) font.Face {
	fnt, _ := truetype.Parse(goregular.TTF)
	return truetype.NewFace(fnt, &truetype.Options{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}
