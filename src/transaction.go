package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"log"
)

//挖出新块的奖励
const subsidy = 10

//对于每一笔交易来说，它的输入都会引用之前一笔交易的输出
//即，将之前一笔交易的输出作为本交易的输入
type Transaction struct {
	ID   []byte
	Vin  []TXInput
	Vout []TXOutput
}

//Txid是之前交易的ID
//Vout存储的是该输出在那笔交易中所有输出的索引
//ScriptSig提供可解锁输出结构中ScriptPubKey字段的数据
type TXInput struct {
	Txid      []byte
	Vout      int
	ScriptSig string
}

//1.一定量的比特币（value）
//2.一个锁定脚本（ScriptPubKey），要花这笔钱，必须要解锁该脚本
type TXOutput struct {
	Value        int
	ScriptPubKey string
}

func (tx Transaction) IsCoinbase() bool {
	return len(tx.Vin) == 1 && len(tx.Vin[0].Txid) == 0 && tx.Vin[0].Vout == -1
}

func (tx Transaction) SetId() {
	var encoded bytes.Buffer
	var hash [32]byte

	enc := gob.NewEncoder(&encoded)
	err := enc.Encode(tx)
	if err != nil {
		log.Panic(err)
	}
	hash = sha256.Sum256(encoded.Bytes())
	tx.ID = hash[:]
}

func (in *TXInput) CanUnlockOutputWith(unlockingData string) bool {
	return in.ScriptSig == unlockingData
}

func (out *TXOutput) CanBeUnlockedWith(unlockingData string) bool {
	return out.ScriptPubKey == unlockingData
}

//当矿工挖出一个新的块时，会向新的块中添加一个coinbase交易
//coinbase交易不需要引用之前一笔交易的输出
func NewCoinbaseTX(to, data string) *Transaction {
	if data == "" {
		data = fmt.Sprintf("Reward to '%s'", to)
	}

	txin := TXInput{[]byte{}, -1, data}
	txout := TXOutput{subsidy, to}
	tx := Transaction{nil, []TXInput{txin}, []TXOutput{txout}}
	tx.SetId()

	return &tx
}

//
func NewUTXOTransaction(from, to string, amount int, bc *BlockChain) *Transaction {
	var inputs []TXInput
	var outputs []TXOutput

	acc, validOutput := bc.FindSpendableOutputs(from, amount)

	if acc < amount {
		log.Panic("ERROR: Not enough funds")
	}

	for txid, outs := range validOutput {
		txID, err := hex.DecodeString(txid)
		if err != nil {
			log.Panic(err)
		}

		for _, out := range outs {
			input := TXInput{txID, out, from}
			inputs = append(inputs, input)
		}
	}

	outputs = append(outputs, TXOutput{amount, to})

	if acc > amount {
		outputs = append(outputs, TXOutput{acc - amount, from})
	}

	tx := Transaction{nil, inputs, outputs}
	tx.SetId()

	return &tx
}
