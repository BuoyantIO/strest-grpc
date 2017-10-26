package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var RootCmd = &cobra.Command{
	Use:   "strest-grpc [client | server]",
	Short: "A load tester for stress testing grpc intermediaries.",
	Long: `A load tester for stress testing grpc intermediaries.

Find more information at https://github.com/buoyantio/strest-grpc.`,
}

// Execute adds all child commands to the root command sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
}
