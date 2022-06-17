package gaia_test

import (
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"

	gaiahelpers "github.com/cosmos/gaia/v8/app/helpers"
)

type EmptyAppOptions struct{}

func (ao EmptyAppOptions) Get(o string) interface{} {
	return nil
}

func TestGaiaApp_BlockedModuleAccountAddrs(t *testing.T) {
	app := gaiahelpers.Setup(t, true, 1)
	blockedAddrs := app.BlockedModuleAccountAddrs()

	// TODO: Blocked on updating to v0.46.x
	// require.NotContains(t, blockedAddrs, authtypes.NewModuleAddress(grouptypes.ModuleName).String())
	require.NotContains(t, blockedAddrs, authtypes.NewModuleAddress(govtypes.ModuleName).String())
}
