package test

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"

	"github.com/fatih/color"
	"github.com/ghodss/yaml"
	"github.com/servehub/serve/manifest"
	"github.com/servehub/utils"
	"github.com/servehub/utils/mergemap"
)

func init() {
	manifest.PluginRegestry.Add("test.component", TestComponent{})
}

type TestComponent struct{}

func (p TestComponent) Run(data manifest.Manifest) error {
	if data.GetString("env") != data.GetString("current-env") {
		log.Printf("No component test found for `%s`.\n", data.GetString("current-env"))
		return nil
	}

	for i, multiComponent := range data.GetArray("components") {
		component := manifest.ParseJSON(data.GetTree("compose.services.component").String())
		merged, _ := mergemap.Merge(component.Unwrap().(map[string]interface{}), multiComponent.Unwrap().(map[string]interface{}))
		name := fmt.Sprintf("component-%d", i+2)

		data.Set("compose.services."+name, merged)
		data.ArrayAppend("compose.services.tests.depends_on", name)
	}

	bytes, finalErr := yaml.Marshal(data.GetTree("compose").Unwrap())
	if finalErr != nil {
		return fmt.Errorf("error on serialize yaml: %v", finalErr)
	}

	tmpfile, finalErr := ioutil.TempFile("", "serve-component-test")
	if finalErr != nil {
		return fmt.Errorf("error create tmpfile: %v", finalErr)
	}

	defer func() {
		tmpfile.Close()
		os.Remove(tmpfile.Name())
	}()

	if _, err := tmpfile.Write(bytes); err != nil {
		return fmt.Errorf("error write to tmpfile: %v", err)
	}

	checkFile := data.GetStringOr("check-file-exist", "")
	if checkFile != "" {
		os.Remove(checkFile)
	}

	log.Printf("docker-compose file:\n%s\n\n", string(bytes))

	if err := utils.RunCmd("docker-compose -p %s -f %s pull", data.GetString("name"), tmpfile.Name()); err != nil {
		return fmt.Errorf("error on pull new images for docker-compose: %v", err)
	}

	defer func() {
		utils.RunCmd("docker-compose -p %s -f %s down -v --remove-orphans", data.GetString("name"), tmpfile.Name())
	}()

	timeout, finalErr := time.ParseDuration(data.GetString("timeout"))
	if finalErr != nil {
		return fmt.Errorf("error parse timeout: %v", finalErr)
	}

	go func() {
		select {
		case <-time.After(timeout):
			color.Red("Timeout exceeded for tests, exit...")
			utils.RunCmd("docker-compose -p %s -f %s down -v --remove-orphans", data.GetString("name"), tmpfile.Name())
		}
	}()

	if res := utils.RunCmd("DOCKER_CLIENT_TIMEOUT=300 COMPOSE_HTTP_TIMEOUT=300 docker-compose -p %s -f %s up --abort-on-container-exit", data.GetString("name"), tmpfile.Name()); res != nil {
		finalErr = fmt.Errorf("error on running docker-compose with tests: %s", res)
	}

	if checkFile != "" && finalErr == nil {
		log.Printf("Check if %s exist...", checkFile)

		if info, err := os.Stat(checkFile); os.IsNotExist(err) || info.Size() < 16 {
			finalErr = fmt.Errorf("check file not exist! %s. %v", checkFile, err)
		}
	}

	return finalErr
}
