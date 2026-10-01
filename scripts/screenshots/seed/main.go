// Command seed fills a throwaway database with demo players and scores for
// the README screenshots. Usage: seed <db path> <fingerprints file>
// where each line of the fingerprints file is "<nick> <SHA256:...>".
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"sshhub/internal/store"
)

// scores lists {trivia, snake} results per player, one entry per round.
var scores = map[string][][2]int{
	"ada":     {{1210, 340}, {980, 210}},
	"linus":   {{1045, 520}, {760, 480}},
	"grace":   {{1320, 150}},
	"ken":     {{640, 610}, {890, 290}},
	"dimitar": {{1150, 430}},
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: seed <db> <fingerprints>")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run(dbPath, fpPath string) error {
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	f, err := os.Open(fpPath)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		nick, fp := fields[0], fields[1]
		if err := st.Register(fp, nick); err != nil {
			return fmt.Errorf("register %s: %w", nick, err)
		}
		for _, r := range scores[nick] {
			if err := st.AddScore(fp, "trivia", r[0]); err != nil {
				return err
			}
			if err := st.AddScore(fp, "snake", r[1]); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}
