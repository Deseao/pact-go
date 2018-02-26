package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pact-foundation/pact-go/dsl"
	"github.com/pact-foundation/pact-go/types"
)

// The actual Provider test itself
func TestPact_Provider(t *testing.T) {
	pact := createPact()

	// Map test descriptions to message producer (handlers)
	// TODO: need to agree on the interface for invoking the function
	//       do we want to pass in args? ...interface{} is a bit of a catch-all
	functionMappings := map[string]func(...interface{}) (interface{}, error){
		"a test message": func(...interface{}) (interface{}, error) {
			fmt.Println("Calling 'text' function that would produce a message")
			res := map[string]interface{}{
				"content": map[string]string{
					"text": "Hello world!!",
				},
			}
			return res, nil
		},
	}

	// Verify the Provider with local Pact Files
	// NOTE: these values don't matter right now,
	// the verifier args are hard coded
	// TODO: Add function mappings to the VerifyRequest type (or have separate one for producer)
	//       this can't happen until we remove the RPC shit, because functions can't be mapped
	//       over the wire
	pact.VerifyProducer(t, types.VerifyRequest{
		ProviderBaseURL:        fmt.Sprintf("http://localhost:%d", port),
		PactURLs:               []string{filepath.ToSlash(fmt.Sprintf("%s/billy-bobby.json", pactDir))},
		ProviderStatesSetupURL: fmt.Sprintf("http://localhost:%d/setup", port),
	}, functionMappings)
}

// Configuration / Test Data
var dir, _ = os.Getwd()
var pactDir = fmt.Sprintf("%s/../../pacts", dir)
var logDir = fmt.Sprintf("%s/log", dir)

// var port, _ = utils.GetFreePort()
var port = 9393

// Setup the Pact client.
func createPact() dsl.Pact {
	// Create Pact connecting to local Daemon
	return dsl.Pact{
		Port:     6666,
		Consumer: "billy",
		Provider: "bobby",
		LogDir:   logDir,
		PactDir:  pactDir,
	}
}
