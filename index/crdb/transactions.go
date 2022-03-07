package crdb

import (
	"context"
	"strings"
	"time"

	"github.com/twcclan/goback/index/crdb/models"
	"github.com/twcclan/goback/proto"

	"github.com/google/uuid"
	"github.com/volatiletech/null/v8"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (i *Index) NewTransaction(ctx context.Context) (*proto.Transaction, error) {
	id := uuid.New()

	tx := &models.Transaction{
		ID:     id.String(),
		Status: models.TransactionStatusOpen,
	}

	err := tx.Insert(ctx, i.db, boil.Infer())
	if err != nil {
		return nil, err
	}

	return transactionModelToProto(tx), nil
}

func (i *Index) SaveTransaction(ctx context.Context, in *proto.Transaction) error {
	tx := transactionProto2Model(in)

	_, err := tx.Update(ctx, i.db, boil.Infer())

	// update the transaction that was passed in
	*in = *transactionModelToProto(tx)

	return err
}

func (i *Index) FindTransaction(ctx context.Context, id string) (*proto.Transaction, error) {
	tx, err := models.FindTransaction(ctx, i.db, id)
	if err != nil {
		return nil, err
	}

	return transactionModelToProto(tx), nil
}

func (i *Index) ListTransactions(ctx context.Context, status proto.Transaction_Status) ([]*proto.Transaction, error) {
	slice, err := models.Transactions(
		models.TransactionWhere.Status.EQ(strings.ToLower(status.String())),
	).All(ctx, i.db)
	if err != nil {
		return nil, err
	}

	txs := make([]*proto.Transaction, len(slice))
	for i, transaction := range slice {
		txs[i] = transactionModelToProto(transaction)
	}

	return txs, nil
}

func transactionModelToProto(tx *models.Transaction) *proto.Transaction {
	status := proto.Transaction_Status(proto.Transaction_Status_value[strings.ToUpper(tx.Status)])

	var updatedAt *timestamppb.Timestamp
	if tx.UpdatedAt.Valid {
		updatedAt = timestamppb.New(tx.UpdatedAt.Time)
	}

	return &proto.Transaction{
		TransactionId: tx.ID,
		Status:        status,
		CreatedAt:     timestamppb.New(tx.CreatedAt),
		UpdatedAt:     updatedAt,
	}
}

func transactionProto2Model(tx *proto.Transaction) *models.Transaction {
	var createdAt time.Time
	var updatedAt null.Time

	if tx.GetCreatedAt() != nil {
		createdAt = tx.GetCreatedAt().AsTime()
	}

	if tx.GetUpdatedAt() != nil {
		updatedAt = null.TimeFrom(tx.GetUpdatedAt().AsTime())
	}

	return &models.Transaction{
		ID:        tx.TransactionId,
		Status:    strings.ToLower(tx.Status.String()),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}
