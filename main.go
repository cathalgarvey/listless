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

	subMode = app.Command("sub", "Without another command, print subscriber list")

	subListMode    = subMode.Command("list", "List subscribers")
	subLConfigFile = subListMode.Arg("configfile", "Location of config file.").Required().String()

	subUpdateAction = subMode.Command("update", "Add or edit a subscriber")
	subUConfigFile  = subUpdateAction.Arg("configfile", "Location of config file").Required().String()
	subUEmail       = subUpdateAction.Flag("email", "Email address to add or update details for").Required().String()
	subUName        = subUpdateAction.Flag("name", "Name of subscriber to add or update details for. Required when adding.").String()
	subUMod         = subUpdateAction.Flag("moderator", "Mark the new/updated user as a moderator").Bool()
	subUPost        = subUpdateAction.Flag("can-post", "Indicate that the new/updated user may post to the list").Bool()

	subRemoveAction = subMode.Command("remove", "Remove a subscriber")
	subRConfigFile  = subRemoveAction.Arg("configfile", "Location of config file").Required().String()
	subREmail       = subRemoveAction.Flag("email", "Email address of user to remove").Required().String()
)

func main() {
	log15.Info("Welcome to Listless!", log15.Ctx{"context": "setup"})
	cmd := kingpin.MustParse(app.Parse(os.Args[1:]))
	switch cmd {
	case loopMode.FullCommand():
		loopModeF()
	case execMode.FullCommand():
		execModeF()
	case subUpdateAction.FullCommand():
		subUpdateModeF()
	case subRemoveAction.FullCommand():
		subRemoveModeF()
	case subListMode.FullCommand():
		subListModeF()
	default:
		log.Fatal("No valid command given. Try '--help' for ideas.")
	}
}

func subUpdateModeF() {
	log15.Info("Starting in subscriber mode", log15.Ctx{"context": "setup"})
	config := loadSettings(*subUConfigFile)
	log15.Info("Loading Engine", log15.Ctx{"context": "setup"})
	engine, err := NewEngine(config)
	if err != nil {
		log15.Error("Failed to load Engine", log15.Ctx{"context": "setup", "error": err})
		log.Fatal(err)
	}
	email := normaliseEmail(*subUEmail)
	if email == "" {
		panic("Provided email address failed to normalise: " + *subUEmail)
	}
	// Does user exist, or is user being added?
	usrmeta, err := engine.DB.GetSubscriber(email)
	switch err {
	case nil:
		{
			// Edit mode
			if usrmeta == nil {
				panic("usrmeta is unexpectedly nil after getting a nil error from DB.GetSubscriber")
			}
			if subUName != nil && *subUName != "" {
				usrmeta.Name = *subUName
			}
			if subUMod != nil {
				usrmeta.Moderator = *subUMod
			}
			if subUPost != nil {
				usrmeta.AllowedPost = *subUPost
			}
			engine.DB.UpdateSubscriber(email, usrmeta)
		}
	case ErrMemberEntryNotFound:
		{
			// Add mode
			name := ""
			isMod := false
			canPost := true
			if subUName == nil {
				panic("Require a name when adding a new member")
			} else {
				name = *subUName
			}
			if subUMod != nil {
				isMod = *subUMod
			}
			if subUPost != nil {
				canPost = *subUPost
			}
			usrmeta := engine.DB.CreateSubscriber(email, name, canPost, isMod)
			engine.DB.UpdateSubscriber(email, usrmeta)
		}
	default:
		{
			panic(err)
		}
	}
}

func subRemoveModeF() {
	// Indempotent for simplicity.
	log15.Info("Starting in subscriber mode", log15.Ctx{"context": "setup"})
	config := loadSettings(*subRConfigFile)
	log15.Info("Loading Engine", log15.Ctx{"context": "setup"})
	engine, err := NewEngine(config)
	if err != nil {
		log15.Error("Failed to load Engine", log15.Ctx{"context": "setup", "error": err})
		log.Fatal(err)
	}
	email := normaliseEmail(*subREmail)
	if email == "" {
		panic("Provided email address failed to normalise: " + *subREmail)
	}
	err = engine.DB.DelSubscriber(email)
	if err != nil {
		panic(err)
	}
}

func subListModeF() {
	log15.Info("Starting in subscriber mode", log15.Ctx{"context": "setup"})
	config := loadSettings(*subLConfigFile)
	log15.Info("Loading Engine", log15.Ctx{"context": "setup"})
	engine, err := NewEngine(config)
	if err != nil {
		log15.Error("Failed to load Engine", log15.Ctx{"context": "setup", "error": err})
		log.Fatal(err)
	}
	fmt.Println("Email,Name,Moderator,AllowedPost")
	engine.DB.forEachSubscriber(func(email string, meta *MemberMeta) error {
		fmt.Printf("%s,%s,%v,%v\n", email, meta.Name, meta.Moderator, meta.AllowedPost)
		return nil
	})
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
