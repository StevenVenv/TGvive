package global

type AppConfig struct {
	Server   ServerConfig   `mapstructure:"server"`
	MySQL    MySQLConfig    `mapstructure:"mysql"`
	JWT      JWTConfig      `mapstructure:"jwt"`
	Telegram TelegramConfig `mapstructure:"telegram"`
}

type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"`
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
	SessionKey  string `mapstructure:"session_key"`
}
