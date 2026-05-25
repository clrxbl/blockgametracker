package config

import (
	"os"
	"sync"

	"github.com/charmbracelet/log"
	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

type Server struct {
	Name     string `yaml:"name"`
	Address  string `yaml:"address"`
	Disabled bool   `yaml:"disabled"`
}

type Config struct {
	Java    []Server `yaml:"java"`
	Bedrock []Server `yaml:"bedrock"`
}

type Store struct {
	mu  sync.RWMutex
	cfg Config
}

func NewStore() *Store { return &Store{} }

func (s *Store) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Store) Load(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	var next Config
	if err := yaml.NewDecoder(file).Decode(&next); err != nil {
		return err
	}

	s.mu.Lock()
	s.cfg = next
	s.mu.Unlock()

	log.Info("loaded config", "java", len(next.Java), "bedrock", len(next.Bedrock))
	return nil
}

func (s *Store) Watch(path string) {
	go func() {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			log.Error("unable to hot reload configuration: " + err.Error())
			return
		}
		defer func() {
			if err := watcher.Close(); err != nil {
				log.Error("failed to close watcher: " + err.Error())
			}
		}()

		if err := watcher.Add(path); err != nil {
			log.Error("unable to hot reload configuration: " + err.Error())
			return
		}

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) {
					log.Info("detected config file change, reloading")
					if err := s.Load(path); err != nil {
						log.Error("failed to reload config: " + err.Error())
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Error("error while watching config file: ", err.Error())
			}
		}
	}()
}

func GetEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
