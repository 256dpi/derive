package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"sort"
	"strings"

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
	fmt.Println("==> Executing rules...")

	// iterate over all rules
	for _, rule := range rules {
		for _, cmd := range rule.Run {
			fmt.Printf("==> Running %s: %q\n", rule.Name, cmd)
			run(cmd, rule.Name)
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
			go func(cmd, name string) {
				for {
					run(cmd, name)
					fmt.Printf("==> Restarting: %s\n", name)
				}
			}(cmd, rule.Name)
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
				// ignore delegates
				if len(rule.Delegate) > 0 {
					continue
				}

				// match globs
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

				// add if included and not excluded
				if inc && !exc {
					dirty[rule] = true
				}
			}
		}

		// check dirty
		if len(dirty) == 0 {
			return
		}

		// log
		fmt.Printf("==> Files changed: %s\n", strings.Join(files, ", "))

		// execute dirty rules
		for rule := range dirty {
			for _, cmd := range rule.Run {
				fmt.Printf("==> Running %s: %q\n", rule.Name, cmd)
				run(cmd, rule.Name)
			}
		}

		// log
		fmt.Println("==> Done!")
	})

	select {}
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

	// prepare queue
	queue := make(chan []string, 100)

	// handle notifications
	go func() {
		for not := range client.Notifications() {
			// check notification
			change, ok := not.(*watchman.ChangeNotification)
			if !ok || change.IsFreshInstance {
				continue
			}

			// get files
			files := make([]string, 0, len(change.Files))
			for _, file := range change.Files {
				files = append(files, file.Name)
			}

			// queue files
			queue <- files
		}
	}()

	// call callback
	go func() {
		for {
			// get notification
			files := <-queue

			// append files
			for len(queue) > 0 {
				files = append(files, <-queue...)
			}

			// yield unique files
			cb(unique(files))
		}
	}()

	// subscribes to all changes
	_, err = watch.Subscribe("derive", path)
	if err != nil {
		panic(err)
	}

	select {}
}

func run(cmd, prefix string) {
	// build command
	c := exec.Command("bash", "-c", cmd)

	// create pipe
	io.Pipe()

	pr, pw := io.Pipe()
	defer pw.Close()

	// set pipes
	c.Stdout = pw
	c.Stderr = pw

	// run printer
	done := make(chan struct{})
	go func() {
		// ensure close
		defer close(done)

		// prepare scanner
		scanner := bufio.NewScanner(pr)

		// print lines
		for scanner.Scan() {
			fmt.Printf("%s: %s\n", prefix, scanner.Text())
		}

		// check error
		err := scanner.Err()
		if err != nil {
			panic(err)
		}
	}()

	// run command
	err := c.Run()
	if err != nil {
		panic(err)
	}

	// closes pipes
	pw.Close()

	// await done
	<-done
}

func unique(list []string) []string {
	// build index
	index := make(map[string]bool, len(list))
	for _, file := range list {
		index[file] = true
	}

	// get uniques
	list = make([]string, 0, len(index))
	for item := range index {
		list = append(list, item)
	}

	// sort
	sort.Strings(list)

	return list
}
