package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var buildVersion = "dev"

func main() {
	check := flag.Bool("check", false, "validate configuration and exit")
	version := flag.Bool("version", false, "print version and exit")
	ledTest := flag.Bool("led-test", false, "show cyan and RGB for 5 seconds without starting audio or HTTP")
	flag.Parse()
	if *version {
		fmt.Println(buildVersion)
		return
	}
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	if *ledTest {
		led := NewLEDController(log.Printf)
		defer led.Set("IDLE")
		if err := led.Set("LISTENING"); err != nil {
			log.Print(err)
			return
		}
		time.Sleep(time.Second)
		if err := led.Set("THINKING"); err != nil {
			log.Print(err)
			return
		}
		time.Sleep(4 * time.Second)
		return
	}
	store, err := NewConfigStore()
	if err != nil {
		log.Fatal(err)
	}
	if *check {
		fmt.Println("configuration ok")
		return
	}
	if err := os.WriteFile("/var/run/assistant-agent.pid", []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644); err != nil {
		log.Printf("pidfile: %v", err)
	}
	defer os.Remove("/var/run/assistant-agent.pid")
	agent := NewAgent(store)
	nativeSpeech, nativeErr := StartNativeSpeechServer(nativeSpeechSocketPath, agent.logf, agent.PrepareNativeSpeechTurn)
	if nativeErr != nil {
		agent.logf("native speech disabled: %v", nativeErr)
	} else {
		agent.SetNativeSpeech(nativeSpeech)
		defer nativeSpeech.Close()
		go func() {
			for turn := range nativeSpeech.Turns() {
				agent.AcceptNativeTurn(turn)
			}
		}()
	}
	server, err := NewHTTPServer(agent, store)
	if err != nil {
		log.Fatal(err)
	}
	cfg, _ := store.Snapshot()
	httpServer := &http.Server{Addr: cfg.ListenAddr, Handler: server.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		log.Printf("assistant-agent %s listening on %s", buildVersion, cfg.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server: %v", err)
		}
	}()
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGUSR1, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	for sig := range signals {
		switch sig {
		case syscall.SIGUSR1:
			if nativeSpeech != nil {
				agent.logf("ignored legacy hotword signal; speech.usock is active")
			} else {
				agent.TriggerWake("hotword")
			}
		case syscall.SIGHUP:
			if err := store.Reload(); err != nil {
				agent.logf("reload failed: %v", err)
			} else {
				agent.logf("configuration reloaded")
			}
		default:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = httpServer.Shutdown(ctx)
			cancel()
			return
		}
	}
}
