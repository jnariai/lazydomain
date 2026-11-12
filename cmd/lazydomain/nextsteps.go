package main

import (
	"fmt"
	"strings"

	"github.com/jnariai/lazydomain/internal/laravel"
	"github.com/jnariai/lazydomain/internal/sites"
	"github.com/jnariai/lazydomain/internal/state"
)

func printNextSteps(p *laravel.Project, site state.Site) {
	steps := sites.NextSteps(p, site)
	fmt.Println()
	if len(steps) == 0 {
		fmt.Printf("✓ %s is already configured for https://%s\n", p.Name(), site.Domain())
		return
	}
	fmt.Printf("Manual changes for %s (lazydomain never edits your project):\n\n", tildify(p.Dir))
	for i, s := range steps {
		fmt.Printf("  %d. %s:\n", i+1, s.Title)
		for _, line := range strings.Split(s.Body, "\n") {
			fmt.Println("       " + line)
		}
		fmt.Println()
	}
	fmt.Println("  Then restart: sail down && sail up -d, and sail npm run dev")
}
