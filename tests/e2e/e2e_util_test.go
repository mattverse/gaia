package e2e

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ory/dockertest/v3/docker"
)

func (s *IntegrationTestSuite) connectIBCChains() {
	s.T().Logf("connecting %s and %s chains via IBC", s.chainA.id, s.chainB.id)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	exec, err := s.dkrPool.Client.CreateExec(docker.CreateExecOptions{
		Context:      ctx,
		AttachStdout: true,
		AttachStderr: true,
		Container:    s.hermesResource.Container.ID,
		User:         "root",
		Cmd: []string{
			"hermes",
			"create",
			"channel",
			s.chainA.id,
			s.chainB.id,
			"--port-a=transfer",
			"--port-b=transfer",
		},
	})
	s.Require().NoError(err)

	var (
		outBuf bytes.Buffer
		errBuf bytes.Buffer
	)

	err = s.dkrPool.Client.StartExec(exec.ID, docker.StartExecOptions{
		Context:      ctx,
		Detach:       false,
		OutputStream: &outBuf,
		ErrorStream:  &errBuf,
	})
	s.Require().NoErrorf(
		err,
		"failed connect chains; stdout: %s, stderr: %s", outBuf.String(), errBuf.String(),
	)

	s.Require().Containsf(
		errBuf.String(),
		"successfully opened init channel",
		"failed to connect chains via IBC: %s", errBuf.String(),
	)

	s.T().Logf("connected %s and %s chains via IBC", s.chainA.id, s.chainB.id)
}

func (s *IntegrationTestSuite) sendMsgSend(c *chain, valIdx int, from, to, amt, fees string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	s.T().Logf("sending %s tokens from %s to %s on chain %s", amt, from, to, c.id)

	exec, err := s.dkrPool.Client.CreateExec(docker.CreateExecOptions{
		Context:      ctx,
		AttachStdout: true,
		AttachStderr: true,
		Container:    s.valResources[c.id][valIdx].Container.ID,
		User:         "root",
		Cmd: []string{
			"gaiad",
			"tx",
			"bank",
			"send",
			from,
			to,
			amt,
			fmt.Sprintf("--%s=%s", flags.FlagChainID, c.id),
			fmt.Sprintf("--%s=%s", flags.FlagFees, fees),
			"--keyring-backend=test",
			"--broadcast-mode=sync",
			"--output=json",
			"-y",
		},
	})
	s.Require().NoError(err)

	var (
		outBuf bytes.Buffer
		errBuf bytes.Buffer
	)

	err = s.dkrPool.Client.StartExec(exec.ID, docker.StartExecOptions{
		Context:      ctx,
		Detach:       false,
		OutputStream: &outBuf,
		ErrorStream:  &errBuf,
	})
	s.Require().NoErrorf(err, "stdout: %s, stderr: %s", outBuf.String(), errBuf.String())

	var txResp sdk.TxResponse
	s.Require().NoError(cdc.UnmarshalJSON(outBuf.Bytes(), &txResp))

	endpoint := fmt.Sprintf("http://%s", s.valResources[c.id][valIdx].GetHostPort("1317/tcp"))

	// wait for the tx to be committed on chain
	s.Require().Eventuallyf(
		func() bool {
			return queryGaiaTx(endpoint, txResp.TxHash) == nil
		},
		time.Minute,
		5*time.Second,
		"stdout: %s, stderr: %s",
		outBuf.String(), errBuf.String(),
	)
}

func (s *IntegrationTestSuite) sendIBC(srcChainID, dstChainID, recipient string, token sdk.Coin) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	s.T().Logf("sending %s from %s to %s (%s)", token, srcChainID, dstChainID, recipient)

	exec, err := s.dkrPool.Client.CreateExec(docker.CreateExecOptions{
		Context:      ctx,
		AttachStdout: true,
		AttachStderr: true,
		Container:    s.hermesResource.Container.ID,
		User:         "root",
		Cmd: []string{
			"hermes",
			"tx",
			"raw",
			"ft-transfer",
			dstChainID,
			srcChainID,
			"transfer",  // source chain port ID
			"channel-0", // since only one connection/channel exists, assume 0
			token.Amount.String(),
			fmt.Sprintf("--denom=%s", token.Denom),
			fmt.Sprintf("--receiver=%s", recipient),
			"--timeout-height-offset=1000",
		},
	})
	s.Require().NoError(err)

	var (
		outBuf bytes.Buffer
		errBuf bytes.Buffer
	)

	err = s.dkrPool.Client.StartExec(exec.ID, docker.StartExecOptions{
		Context:      ctx,
		Detach:       false,
		OutputStream: &outBuf,
		ErrorStream:  &errBuf,
	})
	s.Require().NoErrorf(
		err,
		"failed to send IBC tokens; stdout: %s, stderr: %s", outBuf.String(), errBuf.String(),
	)

	s.T().Log("successfully sent IBC tokens")
}

func (s *IntegrationTestSuite) fundCommunityPool(c *chain, valIdx int, endpoint string, from, amt, fees string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	s.T().Logf("Funding community pool on chain %s", c.id)

	gaiaCommand := []string{
		"gaiad",
		"tx",
		"distribution",
		"fund-community-pool",
		"300000000photon",
		fmt.Sprintf("--%s=%s", flags.FlagFrom, from),
		fmt.Sprintf("--%s=%s", flags.FlagChainID, c.id),
		fmt.Sprintf("--%s=%s", flags.FlagFees, fees),
		"--keyring-backend=test",
		"--output=json",
		"-y",
	}

	s.T().Logf("Executing command: %s", strings.Join(gaiaCommand, " "))
	s.executeGaiaTxCommand(ctx, c, gaiaCommand, valIdx, endpoint)
	s.T().Logf("Successfully funded community pool")
}

func (s *IntegrationTestSuite) submitLegacyGovProposal(c *chain, valIdx int, endpoint string, submitterAddr string, govProposalPath string, fees string, govProposalSubType string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	s.T().Logf("Submitting legacy gov proposal from %s on chain %s", submitterAddr, c.id)

	gaiaCommand := []string{
		"gaiad",
		"tx",
		"gov",
		"submit-legacy-proposal",
		govProposalSubType,
		govProposalPath,
		fmt.Sprintf("--%s=%s", flags.FlagFrom, submitterAddr),
		fmt.Sprintf("--%s=%s", flags.FlagGasPrices, fees),
		fmt.Sprintf("--%s=%s", flags.FlagChainID, c.id),
		"--keyring-backend=test",
		"--output=json",
		"-y",
	}

	s.T().Logf("Gaia Command: %s", strings.Join(gaiaCommand, " "))
	s.executeGaiaTxCommand(ctx, c, gaiaCommand, valIdx, endpoint)
	s.T().Logf("Successfully submitted legacy proposal")
}

func (s *IntegrationTestSuite) depositGovProposal(c *chain, valIdx int, endpoint string, submitterAddr string, proposalId uint64, amount string, fees string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	gaiaCommand := []string{
		"gaiad",
		"tx",
		"gov",
		"deposit",
		fmt.Sprintf("%d", proposalId),
		amount,
		fmt.Sprintf("--%s=%s", flags.FlagFrom, submitterAddr),
		fmt.Sprintf("--%s=%s", flags.FlagGasPrices, fees),
		fmt.Sprintf("--%s=%s", flags.FlagChainID, c.id),
		"--keyring-backend=test",
		"--output=json",
		"-y",
	}

	s.T().Logf("Gaia Command: %s", strings.Join(gaiaCommand, " "))
	s.executeGaiaTxCommand(ctx, c, gaiaCommand, valIdx, endpoint)
	s.T().Logf("Successfully deposited proposal")
}

func (s *IntegrationTestSuite) voteGovProposal(c *chain, valIdx int, endpoint string, submitterAddr string, proposalId uint64, vote string, fees string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	gaiaCommand := []string{
		"gaiad",
		"tx",
		"gov",
		"vote",
		fmt.Sprintf("%d", proposalId),
		vote,
		fmt.Sprintf("--%s=%s", flags.FlagFrom, submitterAddr),
		fmt.Sprintf("--%s=%s", flags.FlagGasPrices, fees),
		fmt.Sprintf("--%s=%s", flags.FlagChainID, c.id),
		"--keyring-backend=test",
		"--output=json",
		"-y",
	}

	s.T().Logf("Gaia Command: %s", strings.Join(gaiaCommand, " "))
	s.executeGaiaTxCommand(ctx, c, gaiaCommand, valIdx, endpoint)
	s.T().Logf("Successfully voted on proposal")
}

func (s *IntegrationTestSuite) submitGovProposal(c *chain, valIdx int, endpoint string, submitterAddr string, govProposalPath string, fees string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	gaiaCommand := []string{
		"gaiad",
		"tx",
		"gov",
		"submit-proposal",
		govProposalPath,
		fmt.Sprintf("--%s=%s", flags.FlagFrom, submitterAddr),
		fmt.Sprintf("--%s=%s", flags.FlagGasPrices, fees),
		fmt.Sprintf("--%s=%s", flags.FlagChainID, c.id),
		"--keyring-backend=test",
		"--output=json",
		"-y",
	}

	s.T().Logf("Gaia Command: %s", strings.Join(gaiaCommand, " "))
	s.executeGaiaTxCommand(ctx, c, gaiaCommand, valIdx, endpoint)
	s.T().Logf("Successfully voted on proposal")
}

func (s *IntegrationTestSuite) executeGaiaTxCommand(ctx context.Context, c *chain, gaiaCommand []string, valIdx int, endpoint string) {
	var (
		outBuf bytes.Buffer
		errBuf bytes.Buffer
		txResp sdk.TxResponse
	)

	s.Require().Eventually(
		func() bool {
			exec, err := s.dkrPool.Client.CreateExec(docker.CreateExecOptions{
				Context:      ctx,
				AttachStdout: true,
				AttachStderr: true,
				Container:    s.valResources[c.id][valIdx].Container.ID,
				User:         "root",
				Cmd:          gaiaCommand,
			})
			s.Require().NoError(err)
			s.T().Logf("Created exec")

			err = s.dkrPool.Client.StartExec(exec.ID, docker.StartExecOptions{
				Context:      ctx,
				Detach:       false,
				OutputStream: &outBuf,
				ErrorStream:  &errBuf,
			})

			s.T().Logf("Out: %s", outBuf.String())
			s.T().Logf("Err: %s", errBuf.String())

			if err != nil {
				s.T().Logf("Error %s", err)
			}

			s.Require().NoError(cdc.UnmarshalJSON(outBuf.Bytes(), &txResp))
			return strings.Contains(txResp.String(), "code: 0")
		},
		5*time.Second,
		time.Second,
		"tx returned a non-zero code; stdout: %s, stderr: %s", outBuf.String(), errBuf.String(),
	)
	s.Require().Eventuallyf(
		func() bool {
			return queryGaiaTx(endpoint, txResp.TxHash) == nil
		},
		time.Minute,
		5*time.Second,
		"stdout: %s, stderr: %s", outBuf.String(), errBuf.String(),
	)
}
