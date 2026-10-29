package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

func setting(values map[string]string, key string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return values[key]
}

func readAuthorRoles(path string) (map[string]map[string]string, error) {
	roles := map[string]map[string]string{}
	if path == "" {
		return roles, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open author roles file")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, fmt.Errorf("cannot read author roles file")
	}
	if err := json.Unmarshal(data, &roles); err != nil {
		return nil, fmt.Errorf("invalid author roles JSON")
	}
	if roles == nil {
		return nil, fmt.Errorf("author roles must be a JSON object")
	}
	for chat, authors := range roles {
		if chat == "" {
			return nil, fmt.Errorf("author roles require chat IDs")
		}
		for author, role := range authors {
			if author == "" || (role != "teacher" && role != "student") {
				return nil, fmt.Errorf("author role must be teacher or student with a profile ID")
			}
		}
	}
	return roles, nil
}
