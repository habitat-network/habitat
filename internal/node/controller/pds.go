package controller

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	types "github.com/eagraf/habitat-new/core/api"

	"github.com/eagraf/habitat-new/internal/node/config"
)

type PDSClientI interface {
	CreateSession(identifier, password string) (types.PDSCreateSessionResponse, error)
	GetInviteCode(nodeConfig *config.NodeConfig) (string, error)
	CreateAccount(nodeConfig *config.NodeConfig, email, handle, password, inviteCode string) (types.PDSCreateAccountResponse, error)
}

type PDSClient struct {
	NodeConfig *config.NodeConfig
}

func (p *PDSClient) GetInviteCode(nodeConfig *config.NodeConfig) (string, error) {
	// Parse the response body to get the invite code
	body, err := p.makePDSHttpReq("com.atproto.server.createInviteCode", http.MethodPost, []byte("{\"useCount\": 1}"), true)
	if err != nil {
		return "", err
	}

	var inviteResponse types.PDSInviteCodeResponse
	err = json.Unmarshal(body, &inviteResponse)
	if err != nil {
		return "", err
	}

	return inviteResponse.Code, nil
}

func (p *PDSClient) CreateAccount(nodeConfig *config.NodeConfig, email, handle, password, inviteCode string) (types.PDSCreateAccountResponse, error) {
	reqBody := types.PDSCreateAccountRequest{
		Email:      email,
		Handle:     handle,
		Password:   password,
		InviteCode: inviteCode,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	respBody, err := p.makePDSHttpReq("com.atproto.server.createAccount", http.MethodPost, []byte(body), false)
	if err != nil {
		return nil, err
	}

	var createAccountResponse types.PDSCreateAccountResponse
	err = json.Unmarshal(respBody, &createAccountResponse)
	if err != nil {
		return nil, err
	}

	return createAccountResponse, nil
}

func (p *PDSClient) CreateSession(identifier, password string) (types.PDSCreateSessionResponse, error) {
	reqBody := types.PDSCreateSessionRequest{
		Identifier: identifier,
		Password:   password,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	respBody, err := p.makePDSHttpReq("com.atproto.server.createSession", http.MethodPost, body, false)
	if err != nil {
		return nil, err
	}

	var createSessionResponse types.PDSCreateSessionResponse
	err = json.Unmarshal(respBody, &createSessionResponse)
	if err != nil {
		return nil, err
	}

	return createSessionResponse, nil
}

// Helper function to make HTTP requests to PDS
func (p *PDSClient) makePDSHttpReq(endpoint, method string, body []byte, isAdminReq bool) ([]byte, error) {
	pdsURL := fmt.Sprintf("http://%s:%s/xrpc/%s", "host.docker.internal", "5001", endpoint)

	req, err := http.NewRequest(method, pdsURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", "application/json")
	if isAdminReq {
		authHeader := basicAuthHeader(p.NodeConfig.PDSAdminUsername(), p.NodeConfig.PDSAdminPassword())
		req.Header.Add("Authorization", authHeader)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("PDS returned status code %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func basicAuthHeader(username, password string) string {
	auth := username + ":" + password
	return fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(auth)))
}
