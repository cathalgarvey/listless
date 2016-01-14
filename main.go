package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"

	"github.com/alecthomas/kingpin"
	"github.com/tgulacsi/imapclient"
	"github.com/yuin/gopher-lua"
)

var (
	// TODO; set two modes, one which runs the list loop, and another that loads
	// DB/Conf and runs arbitrary lua on them once, to assist with setup.
	app            = kingpin.New("listless", "A simple, lua-scripted discussion/mailing list driver over IMAP/SMTP")
	loopMode       = app.Command("loop", "Run the mailing list from a lua configuration file.")
	loopConfigfile = loopMode.Arg("configfile", "Location of config file.").Required().String()

	execMode       = app.Command("exec", "Execute a lua script in the context of a (separate) lua configuration file.")
	execConfigfile = execMode.Arg("configfile", "Location of config file.").Required().String()
	execScript     = execMode.Arg("script", "Location of lua script to execute.").Required().String()
)

func main() {
	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case loopMode.FullCommand():
		{
			loopModeF()
		}
	case execMode.FullCommand():
		{
			execModeF()
		}
	default:
		{
			log.Fatal("No valid command given. Try '--help' for ideas.")
		}
	}
}

func loopModeF() {
	log.Println("Starting Listless in loop mode. Hello!")
	config := loadSettings(*loopConfigfile)
	log.Println("Loading engine..")
	engine, err := NewEngine(config)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Starting event loop.")
	// Setup main loop, run forevs.
	imapclient.DeliveryLoop(engine.Client, "INBOX", "", engine.Handler, "", "", engine.Shutdown)
	log.Println("Exited DeliveryLoop successfully, shutting down.")
}

func execModeF() {
	log.Println("Starting Listless in exec mode. Hello!")
	config := loadSettings(*execConfigfile)
	log.Println("Loading engine..")
	engine, err := NewEngine(config)
	if err != nil {
		log.Fatal(err)
	}
	// Now execute the provided exec script once in the Engine, and quit.
	scriptb, err := ioutil.ReadFile(*execScript)
	if err != nil {
		log.Fatal(err)
	}
	err = engine.ExecOnce(string(scriptb))
	if err != nil {
		log.Fatal(err)
	}
}

func loadSettings(configFile string) *Config {
	log.Println("Reading configuration file: " + configFile)
	configL := lua.NewState()
	configL.DoFile(configFile)
	config := ConfigFromState(configL)
	conf, _ := json.Marshal(config)
	log.Println("Got config file, parsed into settings: " + string(conf))
	return config
}
