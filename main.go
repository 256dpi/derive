package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"

	"github.com/gobwas/glob"
	"github.com/sjansen/watchman"
	"gopkg.in/yaml.v2"
)

var config = flag.String("config", "./derive.yml", "The configuration file.")
var watch = flag.Bool("watch", false, "Watch and derive incrementally.")

type rule struct {
	Name     string   `yaml:"name"`
	Match    []string `yaml:"match"`
	Run      []string `yaml:"run"`
	Delegate []string `yaml:"delegate"`

	incGlobs []glob.Glob
	excGlobs []glob.Glob
}

func main() {
	// parse flags
	flag.Parse()

	// read rules
	buf, err := ioutil.ReadFile(*config)
	if err != nil {
		panic(err)
	}

	// parse rules
	var rules []*rule
	err = yaml.Unmarshal(buf, &rules)
	if err != nil {
		panic(err)
	}

	// compile globs
	for _, rule := range rules {
		for _, match := range rule.Match {
			if match[0] == '!' {
				rule.excGlobs = append(rule.excGlobs, glob.MustCompile(match[1:]))
			} else {
				rule.incGlobs = append(rule.incGlobs, glob.MustCompile(match))
			}
		}
	}

	// log
	fmt.Printf("==> Loaded %d rules from %q\n", len(rules), *config)

	// check config
	for _, rule := range rules {
		// check name
		if rule.Name == "" {
			panic("missing rule name")
		}

		// check marchers
		if len(rule.Delegate) == 0 && len(rule.Match) == 0 {
			panic("missing matchers")
		}

		// check run
		if len(rule.Run) == 0 {
			panic("missing run commands")
		}
	}

	// log
	fmt.Println("==> Deriving...")

	// iterate over all rules
	for _, rule := range rules {
		for _, cmd := range rule.Run {
			fmt.Printf("==> Running %s: %q\n", rule.Name, cmd)
			run(cmd)
		}
	}

	// log
	fmt.Println("==> Done!")

	// return if not watching
	if !*watch {
		return
	}

	// log
	fmt.Println("==> Running delegates...")

	// run delegates
	for _, rule := range rules {
		for _, cmd := range rule.Delegate {
			go run(cmd)
		}
	}

	// log
	fmt.Println("==> Watching files...")

	// get working directory
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	// handle file notifications
	notify(cwd, func(files []string) {
		// match rules
		dirty := map[*rule]bool{}
		for _, file := range files {
			for _, rule := range rules {
				var inc, exc bool
				for _, glb := range rule.incGlobs {
					if glb.Match(file) {
						inc = true
					}
				}
				for _, glb := range rule.excGlobs {
					if glb.Match(file) {
						exc = true
					}
				}
				if inc && !exc {
					dirty[rule] = true
				}
			}
		}

		// execute dirty rules
		for rule := range dirty {
			for _, cmd := range rule.Run {
				fmt.Printf("==> Running %s: %q\n", rule.Name, cmd)
				run(cmd)
			}
		}
	})

	select {}
}

func run(cmd string) {
	// run command
	out, err := exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		print(string(out))
		panic(err)
	}

	// print output
	fmt.Print(string(out))
}

func notify(path string, cb func([]string)) {
	// connect
	client, err := watchman.Connect()
	if err != nil {
		panic(err)
	}

	// ensure close
	defer client.Close()

	// add watch
	watch, err := client.AddWatch(path)
	if err != nil {
		panic(err)
	}

	// TODO: Batch up notifications?

	// print notifications
	go func() {
		for not := range client.Notifications() {
			// get notification
			change, ok := not.(*watchman.ChangeNotification)
			if !ok || change.IsFreshInstance {
				continue
			}

			// get files
			files := make([]string, 0, len(change.Files))
			for _, file := range change.Files {
				files = append(files, file.Name)
			}

			// yield
			cb(files)
		}
	}()

	// subscribes to all changes
	_, err = watch.Subscribe("derive", path)
	if err != nil {
		panic(err)
	}

	select {}
}
