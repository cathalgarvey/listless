package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"gopkg.in/inconshreveable/log15.v2"

	"github.com/alecthomas/kingpin"
	"github.com/yuin/gopher-lua"
)

var (
	app            = kingpin.New("listless", "A simple, lua-scripted discussion/mailing list driver over IMAP/SMTP")
	loopMode       = app.Command("loop", "Run the mailing list from a lua configuration file.")
	loopConfigfile = loopMode.Arg("configfile", "Location of config file.").Required().String()

	execMode       = app.Command("exec", "Execute a lua script in the context of a (separate) lua configuration file.")
	execConfigfile = execMode.Arg("configfile", "Location of config file.").Required().String()
	execScript     = execMode.Arg("script", "Location of lua script to execute.").Required().String()

	subMode       = app.Command("sub", "Add / Remove subscribers to a list.")
	subConfigfile = subMode.Arg("configfile", "Location of config file.").Required().String()
	subAction     = subMode.Arg("action", "Member edit command to use: list | add | update | remove").Required().Enum("add", "update", "remove", "list")
	subAddMod     = subMode.Flag("moderator", "Make new user a moderator").Default("false").Bool()
	subAddPost    = subMode.Flag("can-post", "Allow new user to post").Default("true").Bool()
	subEmail      = subMode.Arg("email", "Email address of new/edited/removed user").String()
	subName       = subMode.Arg("name", "Name of new/edited user").String()
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
	case subMode.FullCommand():
		{
			subModeF()
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

func subModeF() {
	log15.Info("Starting in subscriber mode", log15.Ctx{"context": "setup"})
	config := loadSettings(*subConfigfile)
	log15.Info("Loading Engine", log15.Ctx{"context": "setup"})
	engine, err := NewEngine(config)
	if err != nil {
		log15.Error("Failed to load Engine", log15.Ctx{"context": "setup", "error": err})
		log.Fatal(err)
	}
	switch *subAction {
	case "add":
		{
			if subEmail == nil || subName == nil || *subEmail == "" || *subName == "" {
				panic("Missing email or name argument for new subscriber.")
			}
			log15.Info("Adding user to subscriber", log15.Ctx{"context": "setup", "subEmail": *subEmail, "subName": *subName})
			meta := engine.DB.CreateSubscriber(*subEmail, *subName, *subAddPost, *subAddMod)
			err = engine.DB.UpdateSubscriber(*subEmail, meta)
			if err != nil {
				panic(err)
			}
		}
	case "update":
		{
			if subEmail == nil {
				panic("Missing email argument to update subscriber")
			}
			meta, err := engine.DB.GetSubscriber(*subEmail)
			if err != nil {
				panic(err)
			}
			if subName != nil {
				meta.Name = *subName
			}
			if subAddPost != nil {
				meta.AllowedPost = *subAddPost
			}
			if subAddMod != nil {
				meta.Moderator = *subAddMod
			}
			err = engine.DB.UpdateSubscriber(*subEmail, meta)
			if err != nil {
				panic(err)
			}
		}
	case "remove":
		{
			if subEmail == nil {
				panic("Missing email argument to remove subscriber")
			}
			err = engine.DB.DelSubscriber(*subEmail)
			if err != nil {
				panic(err)
			}
		}
	case "list":
		{
			fmt.Println("Email,Name,Moderator,AllowedPost")
			engine.DB.forEachSubscriber(func(email string, meta *MemberMeta) error {
				fmt.Printf("%s,%s,%v,%v\n", email, meta.Name, meta.Moderator, meta.AllowedPost)
				return nil
			})
		}
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
