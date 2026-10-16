package main

import (
	"auto-check-in/internal/bootstrap"
	"auto-check-in/internal/buildinfo"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

func main() {
	config := bootstrap.EnvConfig()
	flag.StringVar(&config.Listen, "listen", config.Listen, "HTTP listen address")
	flag.StringVar(&config.DataDir, "data", config.DataDir, "Persistent data directory (default: data beside executable)")
	open := flag.Bool("open", false, "Open the management page in the default browser")
	version := flag.Bool("version", false, "Print version")
	health := flag.Bool("healthcheck", false, "Check the running HTTP service")
	flag.Parse()
	if *version {
		fmt.Println(buildinfo.Version)
		return
	}
	if *health {
		host, port, err := net.SplitHostPort(config.Listen)
		if err != nil {
			os.Exit(1)
		}
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		client := http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
		if err != nil {
			os.Exit(1)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	rt, err := bootstrap.Start(config)
	if err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
	defer rt.Close()
	fmt.Println("Management page:", rt.URL)
	fmt.Println("Access token is stored in:", rt.DataDir+string(os.PathSeparator)+"access.token")
	if *open {
		if err = openURL(rt.URL + "/#token=" + rt.Token); err != nil {
			slog.Error("could not open browser")
		}
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	select {
	case <-stop:
	case err := <-rt.Errors:
		slog.Error("HTTP server stopped", "error", err)
	}
}
func openURL(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		command = exec.Command("open", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	return command.Start()
}
