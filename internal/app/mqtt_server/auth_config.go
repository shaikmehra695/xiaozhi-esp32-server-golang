package mqtt_server

import "github.com/spf13/viper"

const (
	defaultAdminUsername = "admin"
	defaultAdminPassword = "test!@#"
)

func configuredAdminUsername() string {
	username := viper.GetString("mqtt_server.username")
	if username == "" {
		return defaultAdminUsername
	}
	return username
}

func configuredAdminPassword() string {
	password := viper.GetString("mqtt_server.password")
	if password == "" {
		return defaultAdminPassword
	}
	return password
}
