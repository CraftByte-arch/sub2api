package repository

import "context"

const usageAggregationMaintenanceLockID int64 = 804326471995013827

func tryUsageAggregationMaintenanceLocks(
	ctx context.Context,
	tx sqlExecutor,
	storeLockID int64,
) (bool, error) {
	var locked bool
	if err := scanSingleRow(ctx, tx, `SELECT pg_try_advisory_xact_lock($1)`, []any{usageAggregationMaintenanceLockID}, &locked); err != nil {
		return false, err
	}
	if !locked {
		return false, nil
	}
	if err := scanSingleRow(ctx, tx, `SELECT pg_try_advisory_xact_lock($1)`, []any{storeLockID}, &locked); err != nil {
		return false, err
	}
	return locked, nil
}

func configureUsageAggregationMaintenance(ctx context.Context, tx sqlExecutor) error {
	if _, err := tx.ExecContext(ctx, `SET LOCAL max_parallel_workers_per_gather = 0`); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout = '20s'`)
	return err
}
