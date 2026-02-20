package global

type AppConfig struct {
	Server    ServerConfig    `mapstructure:"server"`
	MySQL     MySQLConfig     `mapstructure:"mysql"`
	JWT       JWTConfig       `mapstructure:"jwt"`
	Telegram  TelegramConfig  `mapstructure:"telegram"`
	Processor ProcessorConfig `mapstructure:"processor"`
}

type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"`

	// AllowAnonymousDebug permits bypassing auth middleware in debug mode,
	// intended ONLY for local development.
	AllowAnonymousDebug bool `mapstructure:"allow_anonymous_debug"`

	ShutdownTimeoutSec int        `mapstructure:"shutdown_timeout_sec"`
	CORS               CORSConfig `mapstructure:"cors"`
}

type CORSConfig struct {
	AllowOrigins     []string `mapstructure:"allow_origins"`
	AllowMethods     []string `mapstructure:"allow_methods"`
	AllowHeaders     []string `mapstructure:"allow_headers"`
	ExposeHeaders    []string `mapstructure:"expose_headers"`
	AllowCredentials bool     `mapstructure:"allow_credentials"`
	MaxAgeSec        int      `mapstructure:"max_age_sec"`
}

type MySQLConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DBName   string `mapstructure:"dbname"`
	Config   string `mapstructure:"config"`
}

type JWTConfig struct {
	Secret string `mapstructure:"secret"`
}

type TelegramConfig struct {
	APIID       int    `mapstructure:"api_id"`
	APIHash     string `mapstructure:"api_hash"`
	SessionPath string `mapstructure:"session_path"`
}

type ProcessorConfig struct {
	Text  TextProcessorConfig  `mapstructure:"text"`
	Image ImageProcessorConfig `mapstructure:"image"`
	Video VideoProcessorConfig `mapstructure:"video"`
}

type TextProcessorConfig struct {
	Enabled               bool              `mapstructure:"enabled"`
	TrimSpace             bool              `mapstructure:"trim_space"`
	CollapseBlankLines    bool              `mapstructure:"collapse_blank_lines"`
	RemoveLinesContaining []string          `mapstructure:"remove_lines_containing"`
	Replace               []TextReplaceRule `mapstructure:"replace"`
}

type TextReplaceRule struct {
	From string `mapstructure:"from"`
	To   string `mapstructure:"to"`
}

type ImageProcessorConfig struct {
	Enabled       bool            `mapstructure:"enabled"`
	MaxWidth      int             `mapstructure:"max_width"`
	MaxHeight     int             `mapstructure:"max_height"`
	OutputQuality int             `mapstructure:"output_quality"`
	Watermark     WatermarkConfig `mapstructure:"watermark"`
}

type WatermarkConfig struct {
	Enabled   bool    `mapstructure:"enabled"`
	Type      string  `mapstructure:"type"` // text|image
	Text      string  `mapstructure:"text"`
	ImagePath string  `mapstructure:"image_path"`
	FontPath  string  `mapstructure:"font_path"`
	FontSize  float64 `mapstructure:"font_size"`
	Opacity   float64 `mapstructure:"opacity"`  // 0..1
	Position  string  `mapstructure:"position"` // bottom_right|bottom_left|top_right|top_left|center
	Margin    int     `mapstructure:"margin"`
	Scale     float64 `mapstructure:"scale"` // 0..1 (for image watermark)
}

type VideoProcessorConfig struct {
	Enabled            bool    `mapstructure:"enabled"`
	FFmpegPath         string  `mapstructure:"ffmpeg_path"`
	ExtractCover       bool    `mapstructure:"extract_cover"`
	CoverTimestampSec  float64 `mapstructure:"cover_timestamp_sec"`
	CoverMaxWidth      int     `mapstructure:"cover_max_width"`
	CoverMaxHeight     int     `mapstructure:"cover_max_height"`
	CoverOutputQuality int     `mapstructure:"cover_quality"`
}
