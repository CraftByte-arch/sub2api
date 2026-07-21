//go:build unit

package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

type generatedRedeemCodeRepoStub struct {
	RedeemCodeRepository
	created []RedeemCode
}

func (r *generatedRedeemCodeRepoStub) Create(_ context.Context, code *RedeemCode) error {
	clone := *code
	r.created = append(r.created, clone)
	return nil
}

func newAdminServiceGenerateRedeemCodesTestClient(t *testing.T, databaseName string) *dbent.Client {
	t.Helper()

	database, err := sql.Open("sqlite", "file:"+databaseName+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	_, err = database.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	driver := entsql.OpenDB(dialect.SQLite, database)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(driver)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestAdminServiceGenerateUsageCardRedeemCodesUsesPlan(t *testing.T) {
	client := newAdminServiceGenerateRedeemCodesTestClient(t, "admin_generate_redeem_valid")
	ctx := context.Background()
	plan, err := client.UsageCardPlan.Create().
		SetName("Usage 250").
		SetPrice(25).
		SetAmountUsd(250).
		SetValidityDays(30).
		Save(ctx)
	require.NoError(t, err)

	repo := &generatedRedeemCodeRepoStub{}
	service := &adminServiceImpl{redeemCodeRepo: repo, entClient: client}
	planID := int64(plan.ID)
	codes, err := service.GenerateRedeemCodes(ctx, &GenerateRedeemCodesInput{
		Count:           1,
		Type:            RedeemTypeUsageCard,
		Value:           999,
		UsageCardPlanID: &planID,
	})

	require.NoError(t, err)
	require.Len(t, codes, 1)
	require.NotNil(t, codes[0].UsageCardPlanID)
	require.Equal(t, plan.ID, *codes[0].UsageCardPlanID)
	require.Equal(t, plan.AmountUsd, codes[0].Value)
	require.NotNil(t, codes[0].UsageCardPlan)
	require.Equal(t, plan.ID, codes[0].UsageCardPlan.ID)
	require.Len(t, repo.created, 1)
	require.Equal(t, codes[0], repo.created[0])
}

func TestAdminServiceGenerateUsageCardRedeemCodesRejectsInvalidInput(t *testing.T) {
	client := newAdminServiceGenerateRedeemCodesTestClient(t, "admin_generate_redeem_invalid")
	zeroPlanID := int64(0)
	unknownPlanID := int64(999999)
	groupID := int64(7)
	tests := []struct {
		name          string
		input         GenerateRedeemCodesInput
		errorMessage  string
		expectedError error
	}{
		{
			name:         "missing plan",
			input:        GenerateRedeemCodesInput{Count: 1, Type: RedeemTypeUsageCard},
			errorMessage: "usage_card_plan_id is required for usage card type",
		},
		{
			name:         "zero plan",
			input:        GenerateRedeemCodesInput{Count: 1, Type: RedeemTypeUsageCard, UsageCardPlanID: &zeroPlanID},
			errorMessage: "usage_card_plan_id is required for usage card type",
		},
		{
			name:         "group set",
			input:        GenerateRedeemCodesInput{Count: 1, Type: RedeemTypeUsageCard, GroupID: &groupID, UsageCardPlanID: &unknownPlanID},
			errorMessage: "group_id must be empty for usage card type",
		},
		{
			name:         "validity set",
			input:        GenerateRedeemCodesInput{Count: 1, Type: RedeemTypeUsageCard, UsageCardPlanID: &unknownPlanID, ValidityDays: 30},
			errorMessage: "validity_days must be empty for usage card type",
		},
		{
			name:          "plan not found",
			input:         GenerateRedeemCodesInput{Count: 1, Type: RedeemTypeUsageCard, UsageCardPlanID: &unknownPlanID},
			expectedError: ErrUsageCardPlanNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &generatedRedeemCodeRepoStub{}
			service := &adminServiceImpl{redeemCodeRepo: repo, entClient: client}
			_, err := service.GenerateRedeemCodes(context.Background(), &test.input)
			if test.expectedError != nil {
				require.True(t, errors.Is(err, test.expectedError), "unexpected error: %v", err)
			} else {
				require.EqualError(t, err, test.errorMessage)
			}
			require.Empty(t, repo.created)
		})
	}
}
