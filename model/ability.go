package model

import (
	"one-api/common"
	"one-api/common/config"
	"strings"

	"fmt"

	"gorm.io/gorm"
)

type Ability struct {
	Group     string `json:"group" gorm:"type:varchar(32);primaryKey;autoIncrement:false"`
	Model     string `json:"model" gorm:"primaryKey;autoIncrement:false"`
	ChannelId int    `json:"channel_id" gorm:"primaryKey;autoIncrement:false;index"`
	Enabled   bool   `json:"enabled"`
	Priority  *int64 `json:"priority" gorm:"bigint;default:0;index"`
	Weight    *uint  `json:"weight" gorm:"default:1"`
}

func (channel *Channel) AddAbilities() error {
	models := strings.Split(channel.Models, ",")
	group := channel.Group // 直接使用 channel.Group，不再分割

	// 准备批量插入的值
	var values []string
	var args []interface{}

	for _, model := range models {
			values = append(values, "(?, ?, ?, ?, ?, ?)")
			args = append(args, group, model, channel.Id, channel.Status == config.ChannelStatusEnabled, channel.Priority, channel.Weight)
	}

	// 构造 SQL 语句
	sql := fmt.Sprintf(`
			INSERT INTO abilities (`+"`group`"+`, model, channel_id, enabled, priority, weight)
			VALUES %s
			ON DUPLICATE KEY UPDATE
			`+"`group`"+` = VALUES(`+"`group`"+`),
			enabled = VALUES(enabled),
			priority = VALUES(priority),
			weight = VALUES(weight)
	`, strings.Join(values, ","))

	// 执行 SQL
	return DB.Exec(sql, args...).Error
}



func (channel *Channel) DeleteAbilities() error {
	return DB.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
}

// UpdateAbilities updates abilities of this channel.
// Make sure the channel is completed before calling this function.
func (channel *Channel) UpdateAbilities() error {
	// A quick and dirty way to update abilities
	// First delete all abilities of this channel
	err := channel.DeleteAbilities()
	if err != nil {
		return err
	}
	// Then add new abilities
	err = channel.AddAbilities()
	if err != nil {
		return err
	}
	return nil
}

func UpdateAbilityStatus(tx *gorm.DB, channelId int, status bool) error {
	return tx.Model(&Ability{}).Where("channel_id = ?", channelId).Select("enabled").Update("enabled", status).Error
}

type AbilityChannelGroup struct {
	Group      string `json:"group"`
	Model      string `json:"model"`
	Priority   int    `json:"priority"`
	ChannelIds string `json:"channel_ids"`
}

func GetAbilityChannelGroup() ([]*AbilityChannelGroup, error) {
	var abilities []*AbilityChannelGroup

	var channelSql string
	if common.UsingPostgreSQL {
		channelSql = `string_agg("channel_id"::text, ',')`
	} else if common.UsingSQLite {
		channelSql = `group_concat("channel_id", ',')`
	} else {
		channelSql = "GROUP_CONCAT(`channel_id` SEPARATOR ',')"
	}

	trueVal := "1"
	if common.UsingPostgreSQL {
		trueVal = "true"
	}

	err := DB.Raw(`
	SELECT `+quotePostgresField("group")+`, model, priority, `+channelSql+` as channel_ids
	FROM abilities
	WHERE enabled = ?
	GROUP BY `+quotePostgresField("group")+`, model, priority
	ORDER BY priority DESC
	`, trueVal).Scan(&abilities).Error

	return abilities, err
}
