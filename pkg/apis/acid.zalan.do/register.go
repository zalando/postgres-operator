package acidzalando

import "os"

var (
    // GroupName is the group name for the operator CRDs
    GroupName = getEnvWithDefault("POSTGRES_OPERATOR_API_GROUP", "acid.zalan.do")
)

func getEnvWithDefault(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}



