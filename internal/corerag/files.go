package corerag

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// languageExtensions maps language -> recognized file extensions (lowercase).
var languageExtensions = map[string][]string{
	"go":         {".go"},
	"python":     {".py"},
	"typescript": {".ts", ".tsx"},
	"javascript": {".js", ".jsx", ".mjs", ".cjs"},
	"java":       {".java"},
}

// ignoreDirs are directories skipped during the walk (build artifacts, VCS).
var ignoreDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, "out": true, ".next": true, "__pycache__": true,
	".venv": true, "venv": true, "target": true, ".idea": true, ".vscode": true,
}

// detectLanguage guesses the repo's language from the most frequent extension.
func detectLanguage(repoPath string) string {
	counts := map[string]int{}
	_ = filepath.Walk(repoPath, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() && ignoreDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		for lang, exts := range languageExtensions {
			for _, e := range exts {
				if ext == e {
					counts[lang]++
					break
				}
			}
		}
		return nil
	})
	best := ""
	bestN := 0
	for lang, n := range counts {
		if n > bestN {
			best = lang
			bestN = n
		}
	}
	if best == "" {
		best = "go"
	}
	return best
}

// collectSourceFiles returns the repo's source files for the language, sorted
// for deterministic indexing.
func collectSourceFiles(repoPath, lang string) ([]string, error) {
	exts := languageExtensions[lang]
	if exts == nil {
		return nil, nil
	}
	extSet := make(map[string]bool, len(exts))
	for _, e := range exts {
		extSet[e] = true
	}
	var files []string
	err := filepath.Walk(repoPath, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if ignoreDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if extSet[strings.ToLower(filepath.Ext(p))] {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// hashFiles returns a stable hash of the concatenated file contents (a content
// fingerprint used to detect whether a repo has changed since the last index).
func hashFiles(files []string) (string, error) {
	h := sha256.New()
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil {
			continue
		}
		_, _ = h.Write([]byte(f))
		_, _ = h.Write([]byte{0})
		// Modtime + size is cheaper than hashing every file and good enough to
		// detect changes for the MVP. Full content hashing is a future option.
		_, _ = h.Write([]byte(fi.ModTime().UTC().Format("20060102150405")))
		_, _ = h.Write([]byte{0})
		_, _ = fmt.Fprintf(h, "%d", fi.Size())
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// repoID returns a stable repo identifier from its absolute path.
func repoID(repoPath string) string {
	return "repo:" + repoPath
}

// relPath returns fpath relative to repoPath.
func relPath(repoPath, fpath string) string {
	rel, err := filepath.Rel(repoPath, fpath)
	if err != nil {
		return fpath
	}
	return filepath.ToSlash(rel)
}

// readFile reads a file's contents.
func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// parserNames returns a sorted list of registered parser language names.
func parserNames(parsers map[string]Parser) []string {
	names := make([]string, 0, len(parsers))
	for n := range parsers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// resolveEdges rewrites synthetic call target IDs to real symbol IDs by
// matching the callee name against symbols in the repo, and drops call edges
// that cannot be resolved (external/stdlib calls) to keep the graph precise.
// Import edges keep their synthetic "import:<pkg>" targets (they describe
// file -> package dependencies, not symbol -> symbol). The filtered edge
// slice is returned.
func resolveEdges(symbols []Symbol, edges []Edge) []Edge {
	byName := make(map[string]string, len(symbols)) // name -> symbol ID
	for _, s := range symbols {
		byName[s.Name] = s.ID
		// Also index the bare method name so "Type.Method" calls resolve.
		if dot := strings.IndexByte(s.Name, '.'); dot >= 0 {
			byName[s.Name[dot+1:]] = s.ID
		}
	}
	kept := make([]Edge, 0, len(edges))
	for _, e := range edges {
		if e.Kind == EdgeCall {
			callee := strings.TrimPrefix(e.To, "call:")
			if id, ok := byName[callee]; ok {
				e.To = id
				kept = append(kept, e)
			}
			// Unresolved call edges are dropped (external/stdlib calls).
			continue
		}
		kept = append(kept, e)
	}
	return kept
}

// computeDegrees populates InDegree/OutDegree on each symbol from the edges.
func computeDegrees(symbols []Symbol, edges []Edge) {
	in := map[string]int{}
	out := map[string]int{}
	for _, e := range edges {
		if e.From == "" || e.To == "" {
			continue
		}
		out[e.From]++
		in[e.To]++
	}
	for i := range symbols {
		symbols[i].InDegree = in[symbols[i].ID]
		symbols[i].OutDegree = out[symbols[i].ID]
	}
}
