package main

import (
	"encoding/json"
	"log"

	"github.com/alecthomas/kingpin"
	"github.com/tgulacsi/imapclient"
	"github.com/yuin/gopher-lua"
)

var (
	configfile = kingpin.Arg("configfile", "Location of config file.").Required().String()
)

func main() {
	kingpin.Parse()
	log.Println("Starting Listless. Hello!")
	log.Println("Reading configuration file: " + *configfile)
	configL := lua.NewState()
	configL.DoFile(*configfile)
	config := ConfigFromState(configL)
	conf, _ := json.Marshal(config)
	log.Println("Got config file, parsed into settings: " + string(conf))
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
