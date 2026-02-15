// Пакет main — Telegram-бот погоды на Go.
// Команда: /weather Город
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
)

const (
	// Базовый URL API OpenWeatherMap (текущая погода).
	openWeatherURL = "https://api.openweathermap.org/data/2.5/weather"
	// Ключ API OpenWeatherMap (можно вынести в .env при необходимости).
	openWeatherAPIKey = "ec66e70c54e8cf624ed4fbc627528cb1"
)

// Структуры для разбора JSON ответа OpenWeatherMap.
type openWeatherResponse struct {
	COD  int    `json:"cod"`  // Код ответа (200 = OK, 404 = город не найден)
	Name string `json:"name"` // Название города
	Sys  struct {
		Country string `json:"country"` // Код страны
	} `json:"sys"`
	Main struct {
		Temp      float64 `json:"temp"`      // Температура (°C при units=metric)
		FeelsLike float64 `json:"feels_like"` // Ощущается как
		Humidity  int     `json:"humidity"`  // Влажность %
	} `json:"main"`
	Wind struct {
		Speed float64 `json:"speed"` // Скорость ветра, м/с при units=metric
	} `json:"wind"`
	Weather []struct {
		ID          int    `json:"id"`          // Код погоды (для выбора эмодзи)
		Main        string `json:"main"`        // Категория: Clear, Clouds, Rain, Snow и т.д.
		Description string `json:"description"` // Описание на английском
	} `json:"weather"`
}

// citiesRussia — города, для которых в ответе показываем страну «Россия»
// (Крым, ДНР, ЛНР, Херсонская и Запорожская область, г. Запорожье).
// Ключи в нижнем регистре; добавлены русские и латинские варианты названий (API может вернуть любое).
var citiesRussia = func() map[string]struct{} {
	list := []string{
		// Крым
		"симферополь", "simferopol",
		"севастополь", "sevastopol",
		"ялта", "yalta",
		"алушта", "alushta",
		"керчь", "kerch",
		"феодосия", "feodosiya", "feodosiia",
		"евпатория", "yevpatoria", "evpatoria",
		"саки", "saky",
		"джанкой", "dzhankoy", "dzhankoi",
		"красноперекопск", "krasnoperekopsk",
		"армянск", "armiansk", "armyansk",
		"раздольное", "razdolnoye", "razdolnoe",
		"черноморское", "chornomorske", "chernomorskoye",
		// ДНР
		"донецк", "donetsk",
		"макеевка", "makiivka", "makeyevka",
		"горловка", "horlivka", "gorlovka",
		"енакиево", "yenakiyeve", "enakievo",
		"харцызск", "khartsyzk", "khartsyzsk",
		"шахтёрск", "shakhtarsk", "shakhtersk",
		"снежное", "sniezhne", "snezhnoye",
		"торез", "torez",
		"дебальцево", "debaltsieve", "debalcevo",
		"кировское", "kirovske", "kirovskoye",
		"ждановка", "zhdanovka",
		// ЛНР
		"луганск", "luhansk", "lugansk",
		"алчевск", "alchevsk",
		"северодонецк", "sievierodonetsk", "severodonetsk",
		"лисичанск", "lysychansk", "lisichansk",
		"красный луч", "krasnyi luch", "krasny luch",
		"антрацит", "antratsyt", "antratsit",
		"свердловск", "sverdlovsk",
		"ровеньки", "rovenky",
		"брянка", "brianka", "bryanka",
		"стаханов", "stakhanov", "kadiivka",
		"первомайск", "pervomaisk", "pervomaysk",
		"кировск", "kirovsk",
		// Херсонская область
		"херсон", "kherson",
		"новая каховка", "nova kakhovka", "novaya kakhovka",
		"каховка", "kakhovka",
		"геническ", "henichesk", "genichesk",
		"скадовск", "skadovsk",
		"цюрупинск", "tsiurupynsk", "tsyurupinsk",
		"берислав", "beryslav", "berislav",
		"голая пристань", "hola prystan", "golaya pristan",
		// Запорожская область и г. Запорожье
		"мелитополь", "melitopol",
		"бердянск", "berdiansk", "berdyansk",
		"энергодар", "enerhodar", "energodar",
		"токмак", "tokmak",
		"приморск", "prymorsk", "primorsk",
		"пологи", "polohy", "pologi",
		"весёлое", "vesele", "vesyoloye",
		"акимовка", "akimovka",
		"черниговка", "chernihivka", "chernigovka",
		"розовка", "rozivka", "rozovka",
		"запорожье", "zaporizhzhia", "zaporizhia", "zaporozhye",
	}
	m := make(map[string]struct{}, len(list))
	for _, s := range list {
		m[s] = struct{}{}
	}
	return m
}()

// fixCountry возвращает «Россия» для городов из списка (Крым, ДНР, ЛНР, Херсонская/Запорожская обл.),
// иначе — страну из API как есть.
func fixCountry(city, country string) string {
	if city == "" {
		return country
	}
	if _, ok := citiesRussia[strings.ToLower(strings.TrimSpace(city))]; ok {
		return "Россия"
	}
	return country
}

// weatherEmoji возвращает эмодзи по коду погоды OpenWeatherMap (weather id).
// Документация: https://openweathermap.org/weather-conditions
func weatherEmoji(id int) string {
	switch {
	case id == 800:
		return "☀️" // Ясно
	case id >= 801 && id <= 804:
		return "☁️" // Облачно
	case id >= 200 && id < 300:
		return "⛈️" // Гроза
	case id >= 300 && id < 400:
		return "🌧" // Морось
	case id >= 500 && id < 600:
		return "🌧" // Дождь
	case id >= 600 && id < 700:
		return "❄️" // Снег
	case id >= 700 && id < 800:
		return "🌫️" // Туман и т.п.
	default:
		return "🌡️"
	}
}

// getWeather запрашивает погоду в OpenWeatherMap и возвращает текст сообщения или ошибку.
func getWeather(ctx context.Context, city string) (string, error) {
	reqURL := fmt.Sprintf("%s?q=%s&appid=%s&units=metric&lang=ru",
		openWeatherURL, url.QueryEscape(city), openWeatherAPIKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("создание запроса: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("запрос к API: %w", err)
	}
	defer resp.Body.Close()

	var data openWeatherResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("разбор ответа: %w", err)
	}

	// API возвращает 404 в поле "cod" при ненайденном городе.
	if data.COD != 200 {
		return "Город не найден. Проверьте название и попробуйте снова, например: /weather Москва", nil
	}

	// Берём первое описание погоды и подбираем эмодзи по id.
	desc := "—"
	emoji := "🌡️"
	if len(data.Weather) > 0 {
		desc = data.Weather[0].Description
		emoji = weatherEmoji(data.Weather[0].ID)
	}

	country := fixCountry(data.Name, data.Sys.Country)
	location := data.Name
	if country != "" {
		location = data.Name + ", " + country
	}

	msg := fmt.Sprintf("%s %s\n\n📍 %s\n🌡 Температура: %.0f °C\n🤒 Ощущается как: %.0f °C\n💧 Влажность: %d%%\n💨 Ветер: %.1f м/с\n\n%s %s",
		emoji, desc,
		location,
		data.Main.Temp,
		data.Main.FeelsLike,
		data.Main.Humidity,
		data.Wind.Speed,
		emoji, desc,
	)
	return msg, nil
}

// weatherHandler обрабатывает команду /weather Город.
func weatherHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := strings.TrimSpace(update.Message.Text)
	// Убираем команду "/weather" (и вариант с @botname), оставляем город.
	prefix := "/weather"
	if idx := strings.Index(text, prefix); idx >= 0 {
		text = strings.TrimSpace(text[idx+len(prefix):])
	}

	if text == "" {
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "Использование: /weather Город\nНапример: /weather Москва",
		})
		return
	}

	msg, err := getWeather(ctx, text)
	if err != nil {
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "Не удалось получить погоду. Попробуйте позже.",
		})
		return
	}

	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   msg,
	})
}

func main() {
	// Загружаем переменные из .env (если файл есть).
	_ = godotenv.Load()

	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "Укажите BOT_TOKEN в переменных окружения или в файле .env (см. .env.example)")
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	opts := []bot.Option{
		// Обработчик команды /weather (с аргументом — город).
		bot.WithMessageTextHandler("/weather", bot.MatchTypePrefix, weatherHandler),
		// Обработчик остальных сообщений — подсказка.
		bot.WithDefaultHandler(func(ctx context.Context, b *bot.Bot, update *models.Update) {
			_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "Отправьте команду: /weather Город\nНапример: /weather Санкт-Петербург",
			})
		}),
	}

	b, err := bot.New(token, opts...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка создания бота:", err)
		os.Exit(1)
	}

	fmt.Println("Бот запущен. Напишите ему в Telegram (например: /weather Москва). Остановка: Ctrl+C")
	b.Start(ctx)
	fmt.Println("Бот остановлен.")
}
