//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageCardRepositoryConvertCardToBalance_ConcurrentBillingAndRetry(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-card-conversion-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})

	var cardID int64
	err := integrationDB.QueryRowContext(ctx, `
		INSERT INTO user_usage_cards (
			user_id, name, starts_at, expires_at, total_limit_usd, used_usd, status, source, created_at, updated_at
		)
		VALUES ($1, 'conversion test card', NOW() - INTERVAL '1 hour', NOW() + INTERVAL '1 hour', 100, 0, 'active', 'admin', NOW(), NOW())
		RETURNING id
	`, user.ID).Scan(&cardID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM redeem_codes WHERE used_by = $1", user.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM user_usage_cards WHERE id = $1", cardID)
		_ = client.User.DeleteOneID(user.ID).Exec(context.Background())
	})

	repo := NewUsageCardRepository(integrationDB)
	type conversionResult struct {
		conversion *service.UsageCardConversion
		err        error
	}
	type billingResult struct {
		cardID *int64
		err    error
	}

	start := make(chan struct{})
	conversionDone := make(chan conversionResult, 1)
	billingDone := make(chan billingResult, 1)
	go func() {
		<-start
		conversion, err := repo.ConvertCardToBalance(ctx, cardID, 1, "concurrency test")
		conversionDone <- conversionResult{conversion: conversion, err: err}
	}()
	go func() {
		<-start
		tx, err := integrationDB.BeginTx(ctx, nil)
		if err != nil {
			billingDone <- billingResult{err: err}
			return
		}
		defer func() { _ = tx.Rollback() }()

		deductedCardID, err := deductFirstAvailableUsageCard(ctx, tx, user.ID, 30)
		if err == nil {
			err = tx.Commit()
		}
		billingDone <- billingResult{cardID: deductedCardID, err: err}
	}()
	close(start)

	conversion := <-conversionDone
	billing := <-billingDone
	require.NoError(t, conversion.err)
	require.NotNil(t, conversion.conversion)
	if billing.err != nil {
		require.ErrorIs(t, billing.err, service.ErrUsageCardUnavailable)
	} else {
		require.Equal(t, cardID, *billing.cardID)
	}

	var balance, usedUSD float64
	var status string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT u.balance, c.used_usd, c.status
		FROM user_usage_cards c
		JOIN users u ON u.id = c.user_id
		WHERE c.id = $1
	`, cardID).Scan(&balance, &usedUSD, &status))
	require.Equal(t, service.UsageCardStatusCancelled, status)
	require.InDelta(t, 100.0, balance+usedUSD, 0.00000001)
	if billing.err == nil {
		require.InDelta(t, 30.0, usedUSD, 0.00000001)
		require.InDelta(t, 70.0, conversion.conversion.AmountUSD, 0.00000001)
	} else {
		require.InDelta(t, 0.0, usedUSD, 0.00000001)
		require.InDelta(t, 100.0, conversion.conversion.AmountUSD, 0.00000001)
	}

	var codeCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM redeem_codes
		WHERE used_by = $1
			AND notes LIKE $2
	`, user.ID, fmt.Sprintf("usage card #%d converted to long-term balance:%%", cardID)).Scan(&codeCount))
	require.Equal(t, 1, codeCount)

	retry, err := repo.ConvertCardToBalance(ctx, cardID, 1, "retry")
	require.Nil(t, retry)
	require.ErrorIs(t, err, service.ErrUsageCardUnavailable)

	var finalBalance, finalUsedUSD float64
	var finalCodeCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", user.ID).Scan(&finalBalance))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT used_usd FROM user_usage_cards WHERE id = $1", cardID).Scan(&finalUsedUSD))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM redeem_codes
		WHERE used_by = $1
			AND notes LIKE $2
	`, user.ID, fmt.Sprintf("usage card #%d converted to long-term balance:%%", cardID)).Scan(&finalCodeCount))
	require.InDelta(t, balance, finalBalance, 0.00000001)
	require.InDelta(t, usedUSD, finalUsedUSD, 0.00000001)
	require.Equal(t, codeCount, finalCodeCount)
}

func TestUsageCardAmountInvariants_RejectUnsafeActivation(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-card-invariant-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM user_usage_cards WHERE user_id = $1", user.ID)
		_ = client.User.DeleteOneID(user.ID).Exec(context.Background())
	})

	insertCard := func(total, used float64, status string) (int64, error) {
		var id int64
		err := integrationDB.QueryRowContext(ctx, `
			INSERT INTO user_usage_cards (
				user_id, name, starts_at, expires_at, total_limit_usd, used_usd, status, source, created_at, updated_at
			)
			VALUES ($1, 'invariant test card', NOW() - INTERVAL '1 hour', NOW() + INTERVAL '1 hour', $2, $3, $4, 'admin', NOW(), NOW())
			RETURNING id
		`, user.ID, total, used, status).Scan(&id)
		return id, err
	}

	for _, tt := range []struct {
		name  string
		total float64
		used  float64
	}{
		{name: "non-positive total", total: 0, used: 0},
		{name: "negative used", total: 100, used: -1},
		{name: "used exceeds total", total: 100, used: 101},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := insertCard(tt.total, tt.used, service.UsageCardStatusActive)
			require.Error(t, err)
		})
	}

	cardID, err := insertCard(100, -1, service.UsageCardStatusSuspended)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "UPDATE user_usage_cards SET status = $1 WHERE id = $2", service.UsageCardStatusActive, cardID)
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, "UPDATE user_usage_cards SET status = $1 WHERE id = $2", service.UsageCardStatusCancelled, cardID)
	require.NoError(t, err)
}

func TestUsageCardRepositoryConvertCardToBalanceRejectsInvalidStoredAmount(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-card-invalid-stored-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})

	var cardID int64
	err := integrationDB.QueryRowContext(ctx, `
		INSERT INTO user_usage_cards (
			user_id, name, starts_at, expires_at, total_limit_usd, used_usd, status, source, created_at, updated_at
		)
		VALUES ($1, 'invalid stored card', NOW() - INTERVAL '1 hour', NOW() + INTERVAL '1 hour', 100, -1, 'suspended', 'admin', NOW(), NOW())
		RETURNING id
	`, user.ID).Scan(&cardID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM user_usage_cards WHERE id = $1", cardID)
		_ = client.User.DeleteOneID(user.ID).Exec(context.Background())
	})

	repo := NewUsageCardRepository(integrationDB)
	conversion, err := repo.ConvertCardToBalance(ctx, cardID, 1, "")

	require.Nil(t, conversion)
	require.True(t, errors.Is(err, service.ErrUsageCardInvalidBalance))
}
