package main

import (
	"io/ioutil"
	"log"
	"os"

	"github.com/alecthomas/kingpin"
	"github.com/azer/logger"
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
	luaLog = logger.New("lua-eventLoop")
	// Logger for Database operations.
	dbLog = logger.New("database")
	// Logger for Setup/Teardown
	llLog = logger.New("listless")
	// Loggers for IMAP/SMTP errors
	imapLog = logger.New("imap")
	smtpLog = logger.New("smtp")
)

func main() {
	llLog.Info("Welcome to Listless!")
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
	llLog.Info("Starting in loop mode.")
	config := loadSettings(*loopConfigfile)
	llLog.Info("Loading Engine..")
	engine, err := NewEngine(config)
	if err != nil {
		llLog.Error("Failed to load Engine: %s", err.Error())
		log.Fatal(err)
	}
	llLog.Info("Starting event loop.")
	// Setup main loop, run forevs.
	engine.DeliveryLoop(engine.Client, "INBOX", "", engine.Handler, "", "", engine.Shutdown)
	//imapclient.DeliveryLoop(engine.Client, "INBOX", "", engine.Handler, "", "", engine.Shutdown)
	llLog.Info("Exited DeliveryLoop successfully, shutting down.")
}

func execModeF() {
	llLog.Info("Starting in exec mode.")
	config := loadSettings(*execConfigfile)
	llLog.Info("Loading Engine..")
	engine, err := NewEngine(config)
	if err != nil {
		llLog.Error("Failed to load Engine: %s", err.Error())
		log.Fatal(err)
	}
	// Now execute the provided exec script once in the Engine, and quit.
	llLog.Info("Loading script for execution: %s", *execScript)
	scriptb, err := ioutil.ReadFile(*execScript)
	if err != nil {
		llLog.Error("Failed to load script: %s", err.Error())
		log.Fatal(err)
	}
	llLog.Info("Executing script")
	err = engine.ExecOnce(string(scriptb))
	if err != nil {
		llLog.Error("Failed to execute script: %s", err.Error())
		log.Fatal(err)
	}
}

func loadSettings(configFile string) *Config {
	llLog.Info("Reading configuration file: %s", configFile)
	configL := lua.NewState()
	configL.DoFile(configFile)
	config := ConfigFromState(configL)
	//conf, _ := json.Marshal(config)
	confLoggable := config.logAttrs()
	if confLoggable == nil {
		llLog.Error("Tried to log configuration file but failed to create loggable representation. This is probably not a fatal error.")
	} else {
		llLog.Info("Got config file, parsed into settings:", *confLoggable)
	}
	return config
}
