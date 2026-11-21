package router

import (
	"encoding/json"
	"os"
	"strings"
)

// CustomCommand defines a declarative slash command loaded from JSON config.
type CustomCommand struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	PromptTemplate string `json:"prompt_template"`
	AdminOnly      bool   `json:"admin_only"`
}

type customCommandsFile struct {
	Commands []CustomCommand `json:"commands"`
}

// LoadCustomCommands reads declarative custom commands from a JSON file.
// Missing files return an empty list without error so minimal setups work out of the box.
func LoadCustomCommands(path string) ([]CustomCommand, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var parsed customCommandsFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	out := make([]CustomCommand, 0, len(parsed.Commands))
	for _, c := range parsed.Commands {
		name := strings.ToLower(strings.TrimSpace(c.Name))
		if name == "" {
			continue
		}
		if !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		c.Name = name
		out = append(out, c)
	}
	return out, nil
}
