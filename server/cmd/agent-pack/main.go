package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/anbanai/anban-creator/server/agentpack"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "agent-pack:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("expected new, generate, or check")
	}
	switch args[0] {
	case "new":
		flags := flag.NewFlagSet("new", flag.ContinueOnError)
		pluginRoot := flags.String("plugin-root", "plugins", "canonical plugin root")
		id := flags.String("id", "", "kebab-case Pack ID")
		kind := flags.String("kind", agentpack.KindManaged, "plugin or managed")
		taskType := flags.String("task-type", "", "managed task type")
		profile := flags.String("runtime-profile", "", "managed runtime profile")
		adapter := flags.String("adapter", agentpack.AdapterStandard, "managed runtime adapter")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		return agentpack.Scaffold(*pluginRoot, agentpack.ScaffoldOptions{
			ID: *id, Kind: *kind, TaskType: *taskType, RuntimeProfile: *profile, Adapter: *adapter,
		})
	case "generate", "check":
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		pluginRoot := flags.String("plugin-root", "plugins", "canonical plugin root")
		catalogPath := flags.String("catalog", "server/agentpack/catalog.generated.json", "generated Server Catalog")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if args[0] == "check" {
			return agentpack.CheckRepository(*pluginRoot, *catalogPath)
		}
		result, err := agentpack.GenerateRepository(*pluginRoot, *catalogPath)
		if err != nil {
			return err
		}
		fmt.Printf("Agent Packs generated (changed=%t catalog=%s)\n", result.Changed, result.CatalogDigest)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
