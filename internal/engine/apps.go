package engine

type App struct {
	ID   int
	Hash string
}

// Apps stores built-in app credentials.
// Note: Using official client credentials may violate Telegram ToS in some scenarios.
var Apps = map[string]App{
	"desktop": {ID: 2040, Hash: "b18441a1ff607e10a989891a5462e627"}, // Telegram Desktop
	"ios":     {ID: 10840, Hash: "62d8fd6c4a51139158392157057cc61d"},
}
