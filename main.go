package main

import (
	"io/ioutil"
	"log"
	"os"

	"gopkg.in/inconshreveable/log15.v2"

	"github.com/alecthomas/kingpin"
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

	// Logger for the Lua EventLoop.
	//luaLog = logger.New("lua-eventLoop")
	// Logger for Database operations.
	//dbLog = logger.New("database")
	// Logger for Setup/Teardown
	//llLog = logger.New("listless")
	// Loggers for IMAP/SMTP errors
	//imapLog = logger.New("imap")
	//smtpLog = logger.New("smtp")
)

func main() {
	log15.Info("Welcome to Listless!", log15.Ctx{"context": "setup"})
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
	log15.Info("Starting in loop mode", log15.Ctx{"context": "setup"})
	config := loadSettings(*loopConfigfile)
	log15.Info("Loading Engine..", log15.Ctx{"context": "setup"})
	engine, err := NewEngine(config)
	if err != nil {
		log15.Error("Failed to load Engine", log15.Ctx{"context": "setup", "error": err})
		log.Fatal(err)
	}
	log15.Info("Starting event loop", log15.Ctx{"context": "setup"})
	// Setup main loop, run forevs.
	engine.DeliveryLoop(engine.Client, "INBOX", "", engine.Handler, "", "", engine.Shutdown)
	//imapclient.DeliveryLoop(engine.Client, "INBOX", "", engine.Handler, "", "", engine.Shutdown)
	log15.Info("Exited DeliveryLoop successfully, shutting down", log15.Ctx{"context": "teardown"})
}

func execModeF() {
	log15.Info("Starting in exec mode", log15.Ctx{"context": "setup"})
	config := loadSettings(*execConfigfile)
	log15.Info("Loading Engine", log15.Ctx{"context": "setup"})
	engine, err := NewEngine(config)
	if err != nil {
		log15.Error("Failed to load Engine", log15.Ctx{"context": "setup", "error": err})
		log.Fatal(err)
	}
	// Now execute the provided exec script once in the Engine, and quit.
	log15.Info("Loading script for execution", log15.Ctx{"context": "setup", "script": *execScript})
	scriptb, err := ioutil.ReadFile(*execScript)
	if err != nil {
		log15.Error("Failed to load script", log15.Ctx{"context": "setup", "error": err})
		log.Fatal(err)
	}
	log15.Info("Executing script", log15.Ctx{"context": "setup", "script": *execScript})
	err = engine.ExecOnce(string(scriptb))
	if err != nil {
		log15.Error("Failed to execute script", log15.Ctx{"context": "setup", "error": err, "script": *execScript})
		log.Fatal(err)
	}
}

func loadSettings(configFile string) *Config {
	log15.Info("Reading config file", log15.Ctx{"context": "setup", "configFile": configFile})
	configL := lua.NewState()
	configL.DoFile(configFile)
	config := ConfigFromState(configL)
	log15.Info("Got config file, parsed into settings", log15.Ctx{"context": "setup", "configFile": configFile, "settings": config})
	return config
}
