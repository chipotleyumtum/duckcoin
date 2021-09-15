package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/quackduck/duckcoin/util"
)

const (
	BlockchainFile        = "blockchain.json"
	NewestBlockFile       = "newestblock.json"
	BalancesFile          = "balances.json"
	Reward          int64 = 1e6
)

var (
	// this should change based on time taken by each block
	Difficulty, _ = new(big.Int).SetString("0000100000000000000000000000000000000000000000000000000000000000", 16)
	NewestBlock   util.Block
	Balances      = make(map[string]int64)
)

func main() {
	if !fileExists(BlockchainFile) {
		if err := setupNewBlockchain(); err != nil {
			fmt.Println("error: ", err)
			return
		}
	} else {
		if err := setup(); err != nil {
			fmt.Println("error: ", err)
			return
		}
	}
	m := mux.NewRouter()
	m.HandleFunc("/blocks", handleGetBlocks).Methods("GET")
	m.HandleFunc("/balances", handleGetBalances).Methods("GET")
	m.HandleFunc("/blocks/new", handleWriteBlock).Methods("POST")
	m.HandleFunc("/blocks/newest", handleGetNewest).Methods("GET")

	m.HandleFunc("/difficulty", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Sending", Difficulty.Text(16), "to difficulty request")
		_, err := w.Write([]byte(Difficulty.Text(16)))
		if err != nil {
			fmt.Println("error: ", err)
			return
		}
	}).Methods("GET")

	go func() {
		s := &http.Server{
			Addr:           "0.0.0.0:80",
			Handler:        m,
			ReadTimeout:    10 * time.Second,
			WriteTimeout:   10 * time.Second,
			MaxHeaderBytes: 1 << 20,
		}
		if err := s.ListenAndServe(); err != nil {
			fmt.Println(err)
			return
		}
	}()

	fmt.Println("HTTP Server Listening on port 8080")
	s := &http.Server{
		Addr:           "0.0.0.0:8080",
		Handler:        m,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	if err := s.ListenAndServe(); err != nil {
		fmt.Println(err)
		return
	}
}

func setup() error {
	b, err := ioutil.ReadFile(NewestBlockFile)
	if err != nil {
		return err
	}
	err = json.Unmarshal(b, &NewestBlock)
	if err != nil {
		return err
	}
	b, err = ioutil.ReadFile(BalancesFile)
	if err != nil {
		return err
	}
	err = json.Unmarshal(b, &Balances)
	if err != nil {
		return err
	}
	return nil
}

func setupNewBlockchain() error {
	t := time.Now() // genesis time
	genesisBlock := util.Block{
		Index:     0,
		Timestamp: t.Unix(),
		Data:      "Genesis block. Thank you so much to Jason Antwi-Appah for the incredible name that is Duckcoin. QUACK!",
		Hash:      "",
		PrevHash:  "🐤",
		Solution:  "Go Gophers and DUCKS! github.com/quackduck",
		Solver:    "Ishan Goel (quackduck on GitHub)",
		Tx: util.Transaction{
			Data: "Genesis transaction",
		},
	}

	genesisBlock.Hash = util.CalculateHash(genesisBlock)
	fmt.Println(util.ToJSON(genesisBlock))
	f, err := os.Create(BlockchainFile)
	if err != nil {
		return err
	}
	_, err = f.Write([]byte(util.ToJSON([]util.Block{genesisBlock})))
	if err != nil {
		return err
	}
	err = f.Close()
	if err != nil {
		return err
	}

	NewestBlock = genesisBlock
	err = ioutil.WriteFile(NewestBlockFile, []byte(util.ToJSON(NewestBlock)), 0755)
	if err != nil {
		return err
	}
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(BalancesFile, []byte(util.ToJSON(Balances)), 0755)
	if err != nil {
		return err
	}
	return nil
}

func addBlockToChain(b util.Block) {
	Balances[b.Solver] += Reward
	NewestBlock = b
	if b.Tx.Amount > 0 {
		Balances[b.Solver] -= Reward // no reward for a transaction block so we can give that reward to the lnodes (TODO).

		Balances[b.Solver] -= b.Tx.Amount
		Balances[b.Tx.Receiver] += b.Tx.Amount
	}

	err := truncateFile(BlockchainFile, 2) // remove the last two parts (the bracket and the newline)
	if err != nil {
		fmt.Println("Could not truncate", BlockchainFile)
		return
	}
	f, err := os.OpenFile(BlockchainFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0755)
	if err != nil {
		fmt.Println("Could not open", BlockchainFile)
		return
	}
	bytes, err := json.MarshalIndent(b, "   ", "   ")
	if err != nil {
		fmt.Println("Could not marshal block to JSON")
		return
	}
	_, err = f.Write(append(append([]byte(",\n   "), bytes...), "\n]"...))
	if err != nil {
		fmt.Println("Could not write to", BlockchainFile)
	}
	err = f.Close()
	if err != nil {
		fmt.Println("error: ", err)
		return
	}

	err = ioutil.WriteFile(NewestBlockFile, []byte(util.ToJSON(b)), 0755)
	if err != nil {
		fmt.Println("Could not write to", NewestBlockFile)
	}
	err = ioutil.WriteFile(BalancesFile, []byte(util.ToJSON(Balances)), 0755)
	if err != nil {
		fmt.Println("Could not write to", BalancesFile)
	}
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func truncateFile(name string, bytesToRemove int64) error {
	fi, err := os.Stat(name)
	if err != nil {
		return err
	}
	if fi.Size() < bytesToRemove {
		return nil
	}
	return os.Truncate(name, fi.Size()-bytesToRemove)
}

func handleGetBlocks(w http.ResponseWriter, _ *http.Request) {
	f, err := os.Open(BlockchainFile)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = io.Copy(w, f)
	if err != nil {
		fmt.Println("error: ", err)
		return
	}
	_ = f.Close()
}

func handleGetBalances(w http.ResponseWriter, _ *http.Request) {
	balancesNew := make(map[string]float64)

	for address, balance := range Balances {
		balancesNew[address] = float64(balance) / float64(util.MicroquacksPerDuck)
	}

	bytes, err := json.MarshalIndent(balancesNew, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = w.Write(bytes)
	if err != nil {
		fmt.Println("error: ", err)
		return
	}
}

func handleGetNewest(w http.ResponseWriter, _ *http.Request) {
	bytes, err := json.MarshalIndent(NewestBlock, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = w.Write(bytes)
	if err != nil {
		fmt.Println("error: ", err)
		return
	}
}

func handleWriteBlock(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var b util.Block

	decoder := json.NewDecoder(io.LimitReader(r.Body, 1e6))
	if err := decoder.Decode(&b); err != nil {
		fmt.Println("Bad request. This may be caused by a block that is too big (more than 1mb) but these are usually with malicious intent.")
		respondWithJSON(w, http.StatusBadRequest, r.Body)
		return
	}
	fmt.Println(b)
	defer r.Body.Close()

	if err := isValid(b, NewestBlock); err == nil {
		addBlockToChain(b)
	} else {
		respondWithJSON(w, http.StatusBadRequest, "Invalid block. "+err.Error())
		fmt.Println("Rejected block")
		return
	}
	respondWithJSON(w, http.StatusCreated, "Block accepted.")
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	response, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, err = w.Write([]byte("HTTP 500: Internal Server Error: " + err.Error()))
		if err != nil {
			fmt.Println("error: ", err)
			return
		}
		return
	}
	w.WriteHeader(code)
	_, err = w.Write(response)
	if err != nil {
		fmt.Println("error: ", err)
		return
	}
}

func isValid(newBlock, oldBlock util.Block) error {
	const blockDataLimit = 1e3 * 250
	const txDataLimit = 1e3 * 250

	if newBlock.Tx.Amount < 0 {
		return errors.New("Amount is negative")
	}
	if oldBlock.Index+1 != newBlock.Index {
		return errors.New("Index should be " + strconv.FormatInt(oldBlock.Index+1, 10))
	}
	if oldBlock.Hash != newBlock.PrevHash {
		return errors.New("PrevHash should be " + oldBlock.Hash)
	}
	if util.CalculateHash(newBlock) != newBlock.Hash {
		return errors.New("Block Hash is incorrect. This usually happens if your Difficulty is set incorrectly. Restart your miner.")
	}
	if !util.IsHashSolution(newBlock.Hash, Difficulty) {
		return errors.New("Block is not a solution (does not have Difficulty zeros in hash)")
	}
	if len(newBlock.Data) > blockDataLimit {
		return errors.New("Block's Data field is too large. Should be >= 250 kb")
	}
	if len(newBlock.Tx.Data) > txDataLimit {
		return errors.New("Transaction's Data field is too large. Should be >= 250 kb")
	}
	if newBlock.Tx.Amount > 0 {
		if util.DuckToAddress(newBlock.Tx.PubKey) != newBlock.Tx.Sender {
			return errors.New("Pubkey does not match sender address")
		}
		if ok, err := util.CheckSignature(newBlock.Tx.Signature, newBlock.Tx.PubKey, newBlock.Hash); !ok {
			if err != nil {
				return err
			} else {
				return errors.New("Invalid signature")
			}
		}
		if newBlock.Tx.Sender == newBlock.Solver {
			if Balances[newBlock.Tx.Sender] < newBlock.Tx.Amount { // notice that there is no reward for this block's PoW added to the sender's account first
				return errors.New("Insufficient balance")
			}
		} else {
			if Balances[newBlock.Tx.Sender] < newBlock.Tx.Amount {
				return errors.New("Insufficient balance")
			}
		}
	}
	return nil
}
