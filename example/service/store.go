package service

import "github.com/circleci/ex/db"

type Store struct {
	txm *db.TxManager
}

func NewStore(txm *db.TxManager) *Store {
	return &Store{
		txm: txm,
	}
}
