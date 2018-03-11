/*
Package daemon implements the RPC server side interface to remotely manage
external Pact dependencies: The Pact Mock Service and Provider Verification
"binaries."

See https://github.com/pact-foundation/pact-provider-verifier and
https://github.com/bethesque/pact-mock_service for more on the Ruby "binaries".

NOTE: The ultimate goal here is to replace the Ruby dependencies with a shared
library (Pact Reference - (https://github.com/pact-foundation/pact-reference/).
*/
package daemon

// Runs the RPC daemon for remote communication

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"os/signal"

	"github.com/pact-foundation/pact-go/types"
)

// Daemon wraps the commands for the RPC server.
type Daemon struct {
	pactMockSvcManager     Service
	verificationSvcManager Service
	messageSvcManager      Service
	signalChan             chan os.Signal
}

// NewDaemon returns a new Daemon with all instance variables initialised.
func NewDaemon(MockServiceManager Service, verificationServiceManager Service, messageServiceManager Service) *Daemon {
	MockServiceManager.Setup()
	verificationServiceManager.Setup()
	messageServiceManager.Setup()

	return &Daemon{
		pactMockSvcManager:     MockServiceManager,
		verificationSvcManager: verificationServiceManager,
		messageSvcManager:      messageServiceManager,
		signalChan:             make(chan os.Signal, 1),
	}
}

// StartDaemon starts the daemon RPC server.
func (d Daemon) StartDaemon(port int, network string, address string) {
	log.Println("[INFO] daemon - starting daemon on network:", network, "address:", address, "port:", port)

	serv := rpc.NewServer()
	serv.Register(d)

	// Workaround for multiple RPC ServeMux's
	oldMux := http.DefaultServeMux
	mux := http.NewServeMux()
	http.DefaultServeMux = mux

	serv.HandleHTTP(rpc.DefaultRPCPath, rpc.DefaultDebugPath)

	// Workaround for multiple RPC ServeMux's
	http.DefaultServeMux = oldMux

	l, err := net.Listen(network, fmt.Sprintf("%s:%d", address, port))
	if err != nil {
		panic(err)
	}
	go http.Serve(l, mux)

	// Wait for sigterm
	signal.Notify(d.signalChan, os.Interrupt, os.Kill)
	s := <-d.signalChan
	log.Println("[INFO] daemon - received signal:", s, ", shutting down all services")

	d.Shutdown()
}

// StopDaemon allows clients to programmatically shuts down the running Daemon
// via RPC.
func (d Daemon) StopDaemon(request string, reply *string) error {
	log.Println("[DEBUG] daemon - stop daemon")
	d.signalChan <- os.Interrupt
	return nil
}

// Shutdown ensures all services are cleanly destroyed.
func (d Daemon) Shutdown() {
	log.Println("[DEBUG] daemon - shutdown")
	for _, s := range d.verificationSvcManager.List() {
		if s != nil {
			d.pactMockSvcManager.Stop(s.Process.Pid)
		}
	}
}

// StartServer starts a mock server and returns a pointer to a types.MockServer
// struct.
func (d Daemon) StartServer(request types.MockServer, reply *types.MockServer) error {
	log.Println("[DEBUG] daemon - starting mock server with args:", request.Args)
	server := &types.MockServer{}
	svc := d.pactMockSvcManager.NewService(request.Args)
	server.Status = -1
	cmd := svc.Start()
	server.Pid = cmd.Process.Pid
	*reply = *server
	return nil
}

// VerifyProvider runs the Pact Provider Verification Process.
func (d Daemon) VerifyProvider(request types.VerifyRequest, reply *types.ProviderVerifierResponse) error {
	log.Println("[DEBUG] daemon - verifying provider")

	// Convert request into flags, and validate request
	err := request.Validate()
	if err != nil {
		return err
	}

	// Run command, splitting out stderr and stdout. The command can fail for
	// several reasons:
	// 1. Command is unable to run at all.
	// 2. Command runs, but fails for unknown reason.
	// 3. Command runs, and returns exit status 1 because the tests fail.
	//
	// First, attempt to decode the response of the stdout.
	// If that is successful, we are at case 3. Return stdout as message, no error.
	// Else, return an error, include stderr and stdout in both the error and message.
	svc := d.verificationSvcManager.NewService(request.Args)
	cmd := svc.Command()

	stdOutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stdErrPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	err = cmd.Start()
	if err != nil {
		return err
	}
	stdOut, err := ioutil.ReadAll(stdOutPipe)
	if err != nil {
		return err
	}
	stdErr, err := ioutil.ReadAll(stdErrPipe)
	if err != nil {
		return err
	}

	err = cmd.Wait()

	decoder := json.NewDecoder(bytes.NewReader(stdOut))
	dErr := decoder.Decode(&reply)
	if dErr == nil {
		return nil
	}

	if err == nil {
		err = dErr
	}

	return fmt.Errorf("error verifying provider: %s\n\nSTDERR:\n%s\n\nSTDOUT:\n%s", err, stdErr, stdOut)
}

// CreateMessage runs the Pact Message process
func (d Daemon) CreateMessage(request types.PactMessageRequest, reply *types.CommandResponse) error {
	log.Println("[DEBUG] daemon - adding a message")
	res := &types.CommandResponse{}
	svc := d.messageSvcManager.NewService([]string{})
	_, err := svc.Run()

	if err != nil {
		res.Error = err.Error()
	}

	*reply = *res
	return err
}

// ListServers returns a slice of all running types.MockServers.
func (d Daemon) ListServers(request types.MockServer, reply *types.PactListResponse) error {
	log.Println("[DEBUG] daemon - listing mock servers")
	var servers []*types.MockServer

	for port, s := range d.pactMockSvcManager.List() {
		servers = append(servers, &types.MockServer{
			Pid:  s.Process.Pid,
			Port: port,
		})
	}

	*reply = types.PactListResponse{
		Servers: servers,
	}

	return nil
}

// StopServer stops the given mock server.
func (d Daemon) StopServer(request types.MockServer, reply *types.MockServer) error {
	log.Println("[DEBUG] daemon - stopping mock server")
	success, err := d.pactMockSvcManager.Stop(request.Pid)
	if success == true && err == nil {
		request.Status = 0
	} else {
		request.Status = 1
	}
	*reply = request

	return nil
}
