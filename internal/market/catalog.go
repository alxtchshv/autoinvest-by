package market

// Catalog — активы, доступные для копилок. Это реальные инструменты, которые продают
// белорусские операторы криптоплатформ (резиденты ПВТ); цены берутся с мировых бирж.
var Catalog = []Asset{
	{
		Symbol: "USDT", Name: "Tether", Kind: "stable", Risk: 1, Color: "#26A17B",
		Short:       "Цифровой доллар",
		Description: "Стейблкоин, привязанный к доллару США 1:1. Стоимость в рублях меняется вместе с официальным курсом доллара. Подходит, чтобы держать сбережения в долларах без наличных.",
		CoinGecko:   "tether", ListedSince: "2017-01-01",
	},
	{
		Symbol: "PAXG", Name: "PAX Gold", Kind: "gold", Risk: 2, Color: "#C9A227",
		Short:       "Токенизированное золото",
		Description: "Каждый токен обеспечен одной тройской унцией физического золота в хранилищах Лондона. Цена повторяет биржевую цену золота.",
		Binance:     "PAXGUSDT", Bybit: "PAXGUSDT", CoinGecko: "pax-gold", ListedSince: "2020-02-27",
	},
	{
		Symbol: "BTC", Name: "Bitcoin", Kind: "crypto", Risk: 4, Color: "#F7931A",
		Short:       "Крупнейшая криптовалюта",
		Description: "Первая и самая капитализированная криптовалюта с ограниченной эмиссией 21 млн монет. Сильно колеблется в цене: за год возможны и рост в разы, и падение на 50–70%.",
		Binance:     "BTCUSDT", Bybit: "BTCUSDT", CoinGecko: "bitcoin", ListedSince: "2017-08-17",
	},
	{
		Symbol: "ETH", Name: "Ethereum", Kind: "crypto", Risk: 4, Color: "#627EEA",
		Short:       "Платформа смарт-контрактов",
		Description: "Вторая по размеру криптовалюта, на её сети работают тысячи приложений. Волатильность выше, чем у биткоина.",
		Binance:     "ETHUSDT", Bybit: "ETHUSDT", CoinGecko: "ethereum", ListedSince: "2017-08-17",
	},
	{
		Symbol: "TON", Name: "Toncoin", Kind: "crypto", Risk: 5, Color: "#0098EA",
		Short:       "Блокчейн экосистемы Telegram",
		Description: "Монета сети TON, интегрированной в Telegram. Молодой актив с короткой историей и очень высокой волатильностью.",
		Binance:     "TONUSDT", Bybit: "TONUSDT", CoinGecko: "the-open-network", ListedSince: "2024-08-08",
	},
	{
		Symbol: "SOL", Name: "Solana", Kind: "crypto", Risk: 5, Color: "#9945FF",
		Short:       "Быстрый блокчейн",
		Description: "Высокопроизводительный блокчейн. Исторически падал более чем на 90% и многократно восстанавливался — только для тех, кто готов к сильным колебаниям.",
		Binance:     "SOLUSDT", Bybit: "SOLUSDT", CoinGecko: "solana", ListedSince: "2020-08-11",
	},
	// Токенизированные акции: цифровые знаки, цена которых следует за акцией или фондом США.
	// Их выпускают операторы криптоплатформ ПВТ; котировка — цена базового актива на бирже.
	{
		Symbol: "SPY", Name: "S&P 500", Kind: "stock", Risk: 3, Color: "#1F4E8C", ProOnly: true,
		Short:       "500 крупнейших компаний США",
		Description: "Токен, привязанный к фонду SPDR S&P 500. Одна покупка распределяет деньги между 500 крупнейшими публичными компаниями США. В кризисы индекс падал на 30–50%, восстановление занимало годы.",
		Yahoo:       "SPY", ListedSince: "2016-01-01",
	},
	{
		Symbol: "AAPL", Name: "Apple", Kind: "stock", Risk: 3, Color: "#555555", ProOnly: true,
		Short:       "iPhone, Mac и сервисы",
		Description: "Токен, цена которого следует за акциями Apple. Риск одной компании: зависит от продаж iPhone, сервисов и регулирования.",
		Yahoo:       "AAPL", ListedSince: "2016-01-01",
	},
	{
		Symbol: "MSFT", Name: "Microsoft", Kind: "stock", Risk: 3, Color: "#00A4EF", ProOnly: true,
		Short:       "Облака и корпоративное ПО",
		Description: "Токен, цена которого следует за акциями Microsoft: облачная платформа Azure, Office и сервисы по подписке.",
		Yahoo:       "MSFT", ListedSince: "2016-01-01",
	},
	{
		Symbol: "NVDA", Name: "Nvidia", Kind: "stock", Risk: 5, Color: "#76B900", ProOnly: true,
		Short:       "Чипы для искусственного интеллекта",
		Description: "Токен, цена которого следует за акциями Nvidia — главного производителя чипов для ИИ. Цена очень чувствительна к спросу на ИИ, падения на 50% и более уже были.",
		Yahoo:       "NVDA", ListedSince: "2016-01-01",
	},
	{
		Symbol: "TSLA", Name: "Tesla", Kind: "stock", Risk: 5, Color: "#CC0000", ProOnly: true,
		Short:       "Электромобили и энергетика",
		Description: "Токен, цена которого следует за акциями Tesla — одной из самых волатильных крупных компаний США.",
		Yahoo:       "TSLA", ListedSince: "2016-01-01",
	},
	// Токены белорусских компаний с фиксированной доходностью. Пока это модель: конкретные выпуски
	// (эмитент, срок, ставка) появятся после подключения площадки-партнёра, поэтому цена расчётная.
	{
		Symbol: "BYCORP_USD", Name: "Токены компаний РБ, в долларах", Kind: "bond", Risk: 2, Color: "#2E7D5B", ProOnly: true, Indicative: true,
		CouponBP: 800, FaceCurrency: "USD",
		Short:       "Фиксированный доход в долларах",
		Description: "Цифровые знаки белорусских компаний с фиксированной доходностью в долларах. Сейчас это учебная модель с условной ставкой 8% годовых: цена растёт по ставке, биржевых колебаний нет. Реальные выпуски с конкретными эмитентами появятся после подключения площадки-партнёра. Главный риск — дефолт эмитента; досрочно продать можно не всегда.",
		ListedSince: "2026-01-01",
	},
	{
		Symbol: "BYCORP_BYN", Name: "Токены компаний РБ, в рублях", Kind: "bond", Risk: 2, Color: "#8A5A2B", ProOnly: true, Indicative: true,
		CouponBP: 1600, FaceCurrency: "BYN",
		Short:       "Фиксированный доход в рублях",
		Description: "Цифровые знаки белорусских компаний с фиксированной доходностью в белорусских рублях. Сейчас это учебная модель с условной ставкой 16% годовых. Реальные выпуски появятся после подключения площадки-партнёра. Риски — дефолт эмитента и обесценивание рубля.",
		ListedSince: "2026-01-01",
	},
}

// Find ищет актив по символу.
func Find(symbol string) (Asset, bool) {
	for _, a := range Catalog {
		if a.Symbol == symbol {
			return a, true
		}
	}
	return Asset{}, false
}

// Strategy — готовая стратегия копилки.
type Strategy struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Risk        int              `json:"risk"`
	Weights     map[string]int64 `json:"weights"` // базисные пункты, сумма 10000
}

var Strategies = []Strategy{
	{ID: "dollar", Name: "Доллар", Risk: 1, Weights: map[string]int64{"USDT": 10000},
		Description: "Всё в цифровой доллар. Защита от ослабления рубля, без роста сверх курса."},
	{ID: "reserve", Name: "Резерв", Risk: 2, Weights: map[string]int64{"USDT": 6000, "PAXG": 4000},
		Description: "Доллар и золото. Золото исторически дорожает в кризисы, но может долго стоять на месте."},
	{ID: "balanced", Name: "Баланс", Risk: 3, Weights: map[string]int64{"USDT": 5000, "PAXG": 2000, "BTC": 3000},
		Description: "Половина в долларе, остальное — золото и биткоин для роста на длинной дистанции."},
	{ID: "growth", Name: "Рост", Risk: 4, Weights: map[string]int64{"BTC": 6000, "ETH": 2500, "USDT": 1500},
		Description: "Ставка на крупнейшие криптовалюты. Возможны просадки на 50% и больше."},
	{ID: "crypto", Name: "Крипто-рост", Risk: 5, Weights: map[string]int64{"BTC": 5000, "ETH": 3000, "SOL": 2000},
		Description: "Три крупные криптовалюты. Самый высокий потенциал и самые глубокие просадки — горизонт от трёх лет."},
	{ID: "max", Name: "Максимум", Risk: 5, Weights: map[string]int64{"BTC": 4000, "ETH": 2500, "TON": 2000, "SOL": 1500},
		Description: "Только криптовалюты, включая молодые. Только на деньги, которые не жалко потерять."},
}

func FindStrategy(id string) (Strategy, bool) {
	for _, s := range Strategies {
		if s.ID == id {
			return s, true
		}
	}
	return Strategy{}, false
}
