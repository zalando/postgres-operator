// Copyright © 2019 NAME HERE <EMAIL ADDRESS>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// listCmd represents kubectl pg list.
var listCmd = &cobra.Command{
	Use:   "list the resource of type postgresql.",
	Short: "list cmd list all the resources specific to an object.",
	Long: `List all the info specific to an objects.`,
	Run: func(cmd *cobra.Command, args []string) {
		hiiFlag,_ :=cmd.Flags().GetString("HII")
		 list(hiiFlag)
	},
}

// Experimenting with flags
func list(HII string){
	if(HII=="YES") {
		fmt.Println(HII)
		color.Green("We have Green")
		color.Set(color.FgYellow)

		fmt.Println("Existing text will now be in yellow")
		fmt.Printf("This one %s\n", "too")

		color.Unset() // Don't forget to unset
	}
}
func init() {
	listCmd.Flags().StringP("HII","p","NO","SAY HII")
	rootCmd.AddCommand(listCmd)
}
