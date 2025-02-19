package main

import (
	"database/sql"
	"fmt"
	"github.com/360EntSecGroup-Skylar/excelize/v2"
	log "github.com/sirupsen/logrus"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var (
	exportQuarter    = map[string]string{}
	exportNLMKTables = map[string]map[string]string{}
)

func init() {
	exportQuarter = map[string]string{
		"Consolidated sales": "NLMK Group",
		"Key Indicators":     "KEY INDICATORS",
		"CashFlow":           "Consolidated statement of cash flows (in M'USD)",
		"P&L":                "Consolidated statement of profit or loss (in M'USD)",
		"Balance Sheet":      "Consolidated statement of financial position (in M'USD)",
		"RFP":                "RUSSIAN FLAT PRODUCTS",
	}
	exportNLMKTables["Consolidated sales"] = map[string]string{
		"SALES BY PRODUCT":   "consolidated_sales_for_products",
		"SALES BY REGION":    "consolidated_sales_for_products",
		"REVENUE BY PRODUCT": "revenue_structure",
		"REVENUE BY REGION":  "revenue_structure",
	}
	exportNLMKTables["CashFlow"] = map[string]string{
		"MULTIPLIES & OTHER INDICATORS":                                "financial_highlights",
		"CASH FLOWS FROM INVESTING ACTIVITIES":                         "financial_highlights",
		"Payments from settlement of derivative financial instruments": "financial_highlights",
		"Changes in operating assets and liabilities":                  "financial_highlights",
	}
	exportNLMKTables["P&L"] = map[string]string{
		"MULTIPLIES & OTHER INDICATORS": "financial_highlights",
		"PROFIT AND LOSS":               "financial_highlights",
		"EBITDA":                        "financial_highlights",
		"Sales volume, '000 t":          "financial_ratios",
	}
	exportNLMKTables["Key Indicators"] = map[string]string{
		"Profitability": "financial_ratios",
		"Multiples":     "operational_highlights",
	}
	exportNLMKTables["Balance Sheet"] = map[string]string{
		"Current assets": "financial_ratios",
	}
	exportNLMKTables["RFP"] = map[string]string{
		"COST OF SALES": "cost_of_sales_structure",
		"SEGMENT SALES": "revenue_structure",
	}
	exportNLMKTables["Current assets"] = map[string]string{
		"Current assets": "financial_ratios",
	}
}

func getQuarters(rows *[][]string, quarterRowsName string) (map[int]string, []int) {
	quarters := map[int]string{}
	quarterIdxs := []int{}
	quarterRowsFound := false
	for _, row := range *rows {
		for j, cell := range row {
			if len(cell) == 0 {
				continue
			}
			if cell == quarterRowsName {
				quarterRowsFound = true
				continue
			}

			if quarterRowsFound && isQuarter(cell) {
				quarters[j] = strings.Trim(cell, " *")
				quarterIdxs = append(quarterIdxs, j)
			} else {
				re, _ := regexp.Compile("([0-9]M|FY) [0-9]{4}")
				var quarter string
				if re.MatchString(cell) {
					switch cell[0:2] {
					case "3M":
						quarter = "Q1"
					case "6M":
						quarter = "Q2"
					case "9M":
						quarter = "Q3"
					case "FY":
						quarter = "Q4"
					}
					quarters[j] = quarter + " " + cell[3:]
					quarterIdxs = append(quarterIdxs, j)
				}
			}
		}
		//if quarterRowsFound {
		//	break
		//}
	}
	return quarters, quarterIdxs
}

func getElasticTableFromRows(rows *[][]string, tableName string, quarterRowsName string, diff bool) *map[string]map[string]float64 {
	table := make(map[string]map[string]float64)
	quarters, quarterIdxs := getQuarters(rows, quarterRowsName)
	if len(quarterIdxs) == 0 {
		log.Warn("Quarter rows not found name ", quarterRowsName)
	}
	tableNameFound := false
	tableFieldIdx := 0
	tableFieldFound := false
	for _, row := range *rows {
		var tableField string
		for j, cell := range row {
			if !tableNameFound && cell == tableName {
				tableFieldIdx = j
				tableNameFound = true
				// break
			}
			if tableNameFound && j >= tableFieldIdx && cell != "" && len(tableField) == 0 {
				tableField = strings.Trim(cell, " :*")
				table[tableField] = make(map[string]float64)
				tableFieldFound = true
			}
		}
		if tableNameFound && !(len(tableField) > 0) {
			if tableFieldFound {
				log.Debugf("end table row %+v", row)
				break
			}
			//log.Debugf("skip row %+v", row)
			//continue
		}
		if tableNameFound && len(tableField) > 0 {
			valOld := float64(0)
			for _, idx := range quarterIdxs {
				quarterName := quarters[idx]
				val, _ := strconv.ParseFloat(row[idx], 32)
				if diff {
					valNew := val
					if quarterName[0:2] != "Q1" {
						val = valNew - valOld
						log.Debugf("%s =  %d - %d", quarterName, int(valNew), int(valOld))
					} else {
						log.Debugf("%s =  %d", quarterName, int(val))
					}
					valOld = valNew
				}
				table[tableField][quarterName] = val
			}
		}
	}
	log.Debugf("table %+v", table)
	return &table
}

// financial_and_operating_data_1q_2021.xlsx
func loadNlmkData(conn *sql.DB, fileName string) error {
	secCode := "NLMK"
	dataBasePath := "data"
	if dir := os.Getenv("FINANCIAL_DATA_DIR"); dir != "" {
		dataBasePath = dir
	}
	xlsxFile, err := excelize.OpenFile(path.Join(dataBasePath, fileName))
	if err != nil {
		return err
	}
	for sheet, tableNames := range exportNLMKTables {
		rows, err := xlsxFile.GetRows(sheet)
		if err != nil {
			if strings.HasSuffix(err.Error(), "is not exist") {
				log.Warn(err)
				continue
			}
			return err
		}
		for name, table := range tableNames {
			vals := make([]string, TableColumnNums[table])
			for i := 0; i < TableColumnNums[table]; i++ {
				vals = append(vals, "?")
			}
			if err := insertToDB(conn, secCode, "",
				fmt.Sprintf("INSERT INTO %s (*) VALUES (%s)", table, strings.Join(vals, ",")),
				getElasticTableFromRows(&rows, name, exportQuarter[sheet], false),
			); err != nil {
				return err
			}
		}
	}
	return nil
}
