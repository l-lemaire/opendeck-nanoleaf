package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/l-lemaire/opendeck-nanoleaf/internal/config"
)

// pluginID is the OpenDeck plugin directory name, shared with plugin/manifest.json
// and the Makefile.
const pluginID = "com.github.l-lemaire.nanoleaf.sdPlugin"

// plugin dispatches `nanoleaf plugin status` and `nanoleaf plugin debug on|off`.
// These manage the OpenDeck plugin from the CLI, since the plugin itself has
// no command line the user can reach.
func (a *app) plugin(args []string) error {
	if len(args) == 0 {
		return errors.New("plugin: a subcommand is required (status, debug)")
	}
	switch args[0] {
	case "status":
		return a.pluginStatus(args[1:])
	case "debug":
		return a.pluginDebug(args[1:])
	default:
		return fmt.Errorf("plugin: unknown subcommand %q (want: status, debug)", args[0])
	}
}

func (a *app) pluginStatus(args []string) error {
	fs := flag.NewFlagSet("nanoleaf plugin status", flag.ContinueOnError)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logPath, err := config.PluginLogPath()
	if err != nil {
		return err
	}
	userCfg, _ := os.UserConfigDir()
	installPath := filepath.Join(userCfg, "opendeck", "plugins", pluginID)

	installed := "not installed"
	if _, err := os.Stat(filepath.Join(installPath, "manifest.json")); err == nil {
		installed = "installed"
	}
	fmt.Println("Plugin id:     ", pluginID)
	fmt.Println("Install path:  ", installPath, "-", installed)
	fmt.Println("Plugin log:    ", logPath)
	fmt.Println("Debug logging: ", onOff(cfg.PluginDebug), "(nanoleaf plugin debug on|off)")
	return nil
}

func (a *app) pluginDebug(args []string) error {
	if len(args) == 0 {
		return errors.New("plugin debug: on or off is required")
	}
	var enable bool
	switch args[0] {
	case "on":
		enable = true
	case "off":
		enable = false
	default:
		return fmt.Errorf("plugin debug: want on or off, got %q", args[0])
	}
	fs := flag.NewFlagSet("nanoleaf plugin debug "+args[0], flag.ContinueOnError)
	if err := parseFlags(fs, args[1:]); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.PluginDebug = enable
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("Plugin debug logging %s. Takes effect when the plugin next starts (restart OpenDeck).\n", onOff(enable))
	return nil
}
