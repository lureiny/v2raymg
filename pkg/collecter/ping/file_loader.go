package ping

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// FileLoader loads ping nodes from a local YAML file.
type FileLoader struct {
	path string
}

// NewFileLoader creates a new FileLoader.
func NewFileLoader(path string) *FileLoader {
	return &FileLoader{path: path}
}

// nodesFile represents the YAML file structure.
type nodesFile struct {
	Nodes []*PingNodeInfo `yaml:"nodes"`
}

// Load reads and parses the YAML file.
func (l *FileLoader) Load() ([]*PingNodeInfo, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", l.path, err)
	}

	var nf nodesFile
	if err := yaml.Unmarshal(data, &nf); err != nil {
		return nil, fmt.Errorf("parse yaml %q: %w", l.path, err)
	}

	// Set defaults for nodes
	for _, node := range nf.Nodes {
		if len(node.Usage) == 0 {
			node.Usage = []string{"icmp", "tcp"}
		}
		if node.Port == 0 {
			node.Port = 80 // default TCP port for probing
		}
	}

	return nf.Nodes, nil
}

// Name returns the loader name.
func (l *FileLoader) Name() string {
	return "file"
}
