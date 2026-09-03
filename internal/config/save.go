package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// SaveFleet writes the fleet selection back to the configuration file.
//
// Only the two fleet keys are touched. The file is edited as a YAML node tree
// rather than re-marshalled from the Config struct, so every other setting,
// the order they were written in, and the comments explaining them survive
// exactly as the user left them. A tool that quietly reformats the file it was
// given is a tool people stop letting near their configuration.
//
// A missing file is created, along with the directory it belongs in: choosing
// clusters is often the first thing somebody does, and it should not require
// having written a config file first.
func SaveFleet(path string, fleet []string, groups []FleetGroup) error {
	if path == "" {
		return errors.New("no configuration file to write to")
	}

	doc, err := readDocument(path)
	if err != nil {
		return err
	}
	root := doc.Content[0]

	// An empty list is written as an absent key rather than as `fleet: []`.
	// The two mean the same thing to Correlux, and the absent one does not
	// leave the file asserting something the user did not say.
	if err := setKey(root, "fleet", fleet, len(fleet) > 0); err != nil {
		return err
	}
	if err := setKey(root, "fleetGroups", groups, len(groups) > 0); err != nil {
		return err
	}

	return writeDocument(path, doc)
}

// readDocument parses path into a YAML document node, or invents an empty
// mapping when there is nothing there yet.
func readDocument(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		data = nil
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var doc yaml.Node
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		// Empty, whitespace-only, or a file whose top level is not a mapping.
		// The last is a malformed config; Load reports it, and refusing to
		// write over it is better than replacing it with something valid.
		if len(doc.Content) > 0 {
			return nil, fmt.Errorf("%s is not a YAML mapping", path)
		}
		doc = yaml.Node{
			Kind:    yaml.DocumentNode,
			Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}},
		}
	}
	return &doc, nil
}

// setKey replaces the value of key, appends it when it is absent, or removes it
// when keep is false. The key's own comments travel with it.
func setKey(root *yaml.Node, key string, value any, keep bool) error {
	var encoded yaml.Node
	if keep {
		if err := encoded.Encode(value); err != nil {
			return fmt.Errorf("encode %s: %w", key, err)
		}
	}

	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key {
			continue
		}
		if !keep {
			root.Content = append(root.Content[:i], root.Content[i+2:]...)
			return nil
		}
		// The comment belongs to the setting, not to the value that happened
		// to be there when it was written.
		encoded.HeadComment = root.Content[i+1].HeadComment
		encoded.LineComment = root.Content[i+1].LineComment
		root.Content[i+1] = &encoded
		return nil
	}

	if keep {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &encoded)
	}
	return nil
}

// writeDocument renders the tree and replaces the file atomically, so an
// interrupted save leaves the old configuration rather than half of a new one.
func writeDocument(path string, doc *yaml.Node) error {
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("render %s: %w", path, err)
	}

	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	// The file can name clusters and contexts; it is nobody else's business.
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
