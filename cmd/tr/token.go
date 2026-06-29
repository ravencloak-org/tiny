package main

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"

	"github.com/tinyraven/tinyraven/internal/auth"
	"github.com/tinyraven/tinyraven/internal/config"
	"github.com/tinyraven/tinyraven/internal/model"
)

// newTokenCmd builds `tr token` (create / ls / rm). Tokens are stored in Redis
// (ADR 0005); scopes gate access: ADMIN (all), READ:<pipe>, APPEND:<datasource>
// (use "*" for all). Enforced by the API middleware on /v0/pipes and /v0/events.
func newTokenCmd() *cobra.Command {
	tok := &cobra.Command{Use: "token", Short: "Manage API tokens (scoped bearer tokens)"}

	var (
		name    string
		scopes  []string
		reads   []string
		appends []string
		admin   bool
	)
	create := &cobra.Command{
		Use:   "create",
		Short: "Create a scoped token and print its value once",
		RunE: func(cmd *cobra.Command, _ []string) error {
			all := append([]string{}, scopes...)
			if admin {
				all = append(all, "ADMIN")
			}
			for _, r := range reads {
				all = append(all, "READ:"+r)
			}
			for _, a := range appends {
				all = append(all, "APPEND:"+a)
			}
			if len(all) == 0 {
				return fmt.Errorf("a token needs at least one scope (--admin, --read, --append, or --scope)")
			}
			return withStore(cmd.Context(), func(ctx context.Context, s *auth.Store) error {
				val, err := auth.GenerateValue()
				if err != nil {
					return err
				}
				if err := s.Put(ctx, &model.Token{Name: name, Value: val, Scopes: all}); err != nil {
					return err
				}
				fmt.Printf("✓ token %q created with scopes %v\n", name, all)
				fmt.Printf("  %s\n", val)
				fmt.Println("  (store it now — it is not recoverable)")
				return nil
			})
		},
	}
	create.Flags().StringVar(&name, "name", "", "token name (required)")
	create.Flags().BoolVar(&admin, "admin", false, "grant the ADMIN scope (full access)")
	create.Flags().StringSliceVar(&reads, "read", nil, "grant READ on a pipe (repeatable; '*' = all pipes)")
	create.Flags().StringSliceVar(&appends, "append", nil, "grant APPEND on a datasource (repeatable; '*' = all)")
	create.Flags().StringSliceVar(&scopes, "scope", nil, "raw scope string (repeatable)")
	_ = create.MarkFlagRequired("name")

	ls := &cobra.Command{
		Use:   "ls",
		Short: "List tokens (names + scopes; values are not shown)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withStore(cmd.Context(), func(ctx context.Context, s *auth.Store) error {
				toks, err := s.List(ctx)
				if err != nil {
					return err
				}
				for _, t := range toks {
					fmt.Printf("%-24s %v\n", t.Name, t.Scopes)
				}
				return nil
			})
		},
	}

	rm := &cobra.Command{
		Use:   "rm <name>",
		Short: "Delete a token by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(cmd.Context(), func(ctx context.Context, s *auth.Store) error {
				ok, err := s.DeleteByName(ctx, args[0])
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("no token named %q", args[0])
				}
				fmt.Printf("✓ deleted token %q\n", args[0])
				return nil
			})
		},
	}

	tok.AddCommand(create, ls, rm)
	return tok
}

// withStore opens a Redis-backed token store from config and runs fn.
func withStore(ctx context.Context, fn func(context.Context, *auth.Store) error) error {
	cfg := config.Load()
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer rdb.Close()
	return fn(ctx, auth.NewStore(rdb))
}
