package chaincfg

import "time"

const (
	Coin             int64 = 100_000_000
	Cent             int64 = 1_000_000
	MaxMoney         int64 = 21_000_000 * Coin
	CoinbaseMaturity       = 100

	TargetSpacing   = 10 * time.Minute
	MaxFutureDrift  = 2 * time.Hour
	Subsidy         = 50 * Coin
	HalvingInterval = 210_000

	PublicKeyHashVersion byte = 48
	PrivateKeyVersion    byte = 176
)

var MainNet = Params{
	Name:             "mainnet",
	ChainID:          "legacy-mainnet-1.0.0-rc2-5b4c78e4",
	CoinName:         "Legacy Coin",
	Ticker:           "LBTC",
	MessageStart:     [4]byte{0xa4, 0xac, 0xc6, 0x4d},
	DefaultPort:      19555,
	RPCPort:          19556,
	DNSSeeds:         []string{"legacycoinseed.space", "legacycoinseed2.space"},
	YespowerPers:     "LegacyCoinPoW",
	GenesisTimestamp: "onecpuonevote Legacy Coin Public Mainnet 20/May/2026",
	GenesisTime:      1779235200,
	GenesisBits:      0x207fffff,
	PostGenesisBits:  0x1f0fffff,
	GenesisNonce:     3,
	GenesisHash:      "5b4c78e4556afcd51acf7b9eb2e387fbea2d1414e6042d80d38e6256987154f5",
}

var TestNet = Params{
	Name:             "testnet",
	ChainID:          "legacy-testnet-v5.12",
	CoinName:         "Legacy Coin Testnet",
	Ticker:           "tLBTC",
	MessageStart:     [4]byte{0x0b, 0x11, 0x09, 0x07},
	DefaultPort:      19655,
	RPCPort:          19656,
	DNSSeeds:         []string{"testnet-seed.legacycoinseed.space"},
	YespowerPers:     "LegacyCoinPoWTestnet",
	GenesisTimestamp: "onecpuonevote Legacy Coin Testnet 02/May/2026",
	GenesisTime:      uint32(time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC).Unix()),
	GenesisBits:      0x1e7fffff,
	PostGenesisBits:  0x1e7fffff,
	GenesisNonce:     0,  // To be mined
	GenesisHash:      "", // To be mined
}

type Params struct {
	Name             string
	ChainID          string
	CoinName         string
	Ticker           string
	MessageStart     [4]byte
	DefaultPort      uint16
	RPCPort          uint16
	DNSSeeds         []string
	YespowerPers     string
	GenesisTimestamp string
	GenesisTime      uint32
	GenesisBits      uint32
	PostGenesisBits  uint32
	GenesisNonce     uint32
	GenesisHash      string
}

func BlockSubsidy(height int32) int64 {
	if height < 0 {
		return 0
	}
	halvings := uint(height / HalvingInterval)
	if halvings >= 64 {
		return 0
	}
	return Subsidy >> halvings
}

func MoneyRange(v int64) bool {
	return v >= 0 && v <= MaxMoney
}
