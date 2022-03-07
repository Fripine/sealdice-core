package dice

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func SetBotOnAtGroup(session *IMSession, msg *Message) {
	group := session.ServiceAt[msg.GroupId]
	if group != nil {
		group.Active = true
	} else {
		extLst := []*ExtInfo{}
		for _, i := range session.Parent.ExtList {
			if i.AutoActive {
				extLst = append(extLst, i)
			}
		}
		session.ServiceAt[msg.GroupId] = &ServiceAtItem{
			Active:           true,
			ActivatedExtList: extLst,
			Players:          map[int64]*PlayerInfo{},
			GroupId:          msg.GroupId,
			ValueMap:         map[string]VMValue{},
		}
	}
}

/** 这几条指令不能移除 */
func (d *Dice) registerCoreCommands() {
	cmdHelp := &CmdItemInfo{
		Name: "help",
		Help: ".help // 查看本帮助",
		Solve: func(ctx *MsgContext, msg *Message, cmdArgs *CmdArgs) CmdExecuteResult {
			if ctx.IsCurGroupBotOn {
				text := "SealDice " + VERSION + "\n"
				text += "-----------------------------------------------\n"
				text += "核心指令列表如下:\n"

				used := map[*CmdItemInfo]bool{}
				keys := make([]string, 0, len(d.CmdMap))
				for k, v := range d.CmdMap {
					if used[v] {
						continue
					}
					keys = append(keys, k)
					used[v] = true
				}
				sort.Strings(keys)

				for _, i := range keys {
					i := d.CmdMap[i]
					if i.Help != "" {
						text += i.Help + "\n"
					} else {
						brief := i.Brief
						if brief != "" {
							brief = "   // " + brief
						}
						text += "." + i.Name + brief + "\n"
					}
				}

				text += "注意：由于篇幅此处仅列出核心指令。\n"
				text += "扩展指令请输入 .ext 和 .ext <扩展名称> 进行查看\n"
				text += "-----------------------------------------------\n"
				text += "SealDice 目前 7*24h 运行于一块陈年OrangePi卡片电脑上，随时可能因为软硬件故障停机（例如过热、被猫打翻）。届时可以来Q群524364253询问。"
				ReplyToSender(ctx, msg, text)
			}
			return CmdExecuteResult{true}
		},
	}
	d.CmdMap["help"] = cmdHelp

	cmdBot := &CmdItemInfo{
		Name:  "bot on/off/about/bye",
		Brief: "开启、关闭、查看信息、退群",
		Help:  ".bot on/off/about/bye // 开启、关闭、查看信息、退群",
		Solve: func(ctx *MsgContext, msg *Message, cmdArgs *CmdArgs) CmdExecuteResult {
			inGroup := msg.MessageType == "group"

			if len(cmdArgs.Args) == 0 || cmdArgs.IsArgEqual(1, "about") {
				count := 0
				for _, i := range d.ImSession.ServiceAt {
					if i.Active {
						count += 1
					}
				}
				lastSavedTimeText := "从未"
				if d.LastSavedTime != nil {
					lastSavedTimeText = d.LastSavedTime.Format("2006-01-02 15:04:05") + " UTC"
				}
				text := fmt.Sprintf("SealDice %s\n兼容模式: 已开启\n供职于%d个群，其中%d个处于开启状态\n上次自动保存时间: %s", VERSION, len(d.ImSession.ServiceAt), count, lastSavedTimeText)

				if inGroup {
					if cmdArgs.AmIBeMentioned {
						ReplyGroup(ctx, msg.GroupId, text)
					}
				} else {
					ReplyPerson(ctx, msg.Sender.UserId, text)
				}
			} else {
				if inGroup && cmdArgs.AmIBeMentioned {
					if len(cmdArgs.Args) >= 1 {
						if cmdArgs.Args[0] == "on" {
							SetBotOnAtGroup(ctx.Session, msg)
							ctx.Group = ctx.Session.ServiceAt[msg.GroupId]
							ctx.IsCurGroupBotOn = true
							// "SealDice 已启用(开发中) " + VERSION
							ReplyGroup(ctx, msg.GroupId, DiceFormatTmpl(ctx, "核心:骰子开启"))
						} else if cmdArgs.Args[0] == "off" {
							if len(ctx.Group.ActivatedExtList) == 0 {
								delete(ctx.Session.ServiceAt, msg.GroupId)
							} else {
								ctx.Group.Active = false
							}
							// 停止服务
							ReplyGroup(ctx, msg.GroupId, DiceFormatTmpl(ctx, "核心:骰子关闭"))
						} else if cmdArgs.Args[0] == "bye" {
							// 收到指令，5s后将退出当前群组
							ReplyGroup(ctx, msg.GroupId, DiceFormatTmpl(ctx, "核心:骰子退群预告"))
							ctx.Group.Active = false
							time.Sleep(6 * time.Second)
							QuitGroup(ctx, msg.GroupId)
						} else if cmdArgs.Args[0] == "Save" {
							d.Save(false)
							// 数据已保存
							ReplyGroup(ctx, msg.GroupId, DiceFormatTmpl(ctx, "核心:骰子保存设置"))
						}
					}
				}
			}

			return CmdExecuteResult{true}
		},
	}
	d.CmdMap["bot"] = cmdBot

	cmdRoll := &CmdItemInfo{
		Name: "r <表达式> <原因>",
		Help: ".r <表达式> <原因> // 骰点指令\n.rh <表达式> <原因> // 暗骰",
		Solve: func(ctx *MsgContext, msg *Message, cmdArgs *CmdArgs) CmdExecuteResult {
			if ctx.IsCurGroupBotOn {
				var text string
				var prefix string
				var diceResult int64
				var diceResultExists bool
				var detail string
				disableLoadVarname := !(cmdArgs.Command == "rx" || cmdArgs.Command == "rhx")

				if ctx.Dice.CommandCompatibleMode {
					if (cmdArgs.Command == "rd" || cmdArgs.Command == "rhd") && len(cmdArgs.Args) >= 1 {
						if m, _ := regexp.MatchString(`^\d`, cmdArgs.Args[0]); m {
							cmdArgs.Args[0] = "d" + cmdArgs.Args[0]
						}
					}
				} else {
					return CmdExecuteResult{false}
				}

				forWhat := ""
				var r *VmResult
				if len(cmdArgs.Args) >= 1 {
					var err error
					r, detail, err = ctx.Dice.ExprEvalBase(cmdArgs.Args[0], ctx, false, disableLoadVarname)

					if r != nil && r.TypeId == 0 {
						diceResult = r.Value.(int64)
						diceResultExists = true
						//return errors.New("错误的类型")
					}

					if err == nil {
						if len(cmdArgs.Args) >= 2 {
							forWhat = cmdArgs.Args[1]
						}
					} else {
						errs := string(err.Error())
						if strings.HasPrefix(errs, "E1:") {
							ReplyGroup(ctx, msg.GroupId, errs)
							return CmdExecuteResult{true}
						}
						forWhat = cmdArgs.Args[0]
					}
				}

				if forWhat != "" {
					prefix = "为了" + forWhat + "，"
				}

				if diceResultExists {
					detailWrap := ""
					if detail != "" {
						detailWrap = "=" + detail
					}
					text = fmt.Sprintf("%s<%s>掷出了 %s%s=%d", prefix, ctx.Player.Name, cmdArgs.Args[0], detailWrap, diceResult)
				} else {
					dicePoints := ctx.Player.DiceSideNum
					if dicePoints <= 0 {
						dicePoints = 100
					}
					val := DiceRoll(dicePoints)
					text = fmt.Sprintf("%s<%s>掷出了 D%d=%d", prefix, ctx.Player.Name, dicePoints, val)
				}

				if kw := cmdArgs.GetKwarg("asm"); r != nil && kw != nil {
					asm := r.Parser.GetAsmText()
					text += "\n" + asm
				}

				if cmdArgs.Command == "rh" || cmdArgs.Command == "rhd" {
					prefix := fmt.Sprintf("来自群<%s>(%d)的暗骰，", ctx.Group.GroupName, msg.GroupId)
					ReplyGroup(ctx, msg.GroupId, DiceFormatTmpl(ctx, "核心:暗骰-群内"))
					ReplyPerson(ctx, msg.Sender.UserId, prefix+text)
				} else {
					ReplyGroup(ctx, msg.GroupId, text)
				}
			}
			return CmdExecuteResult{true}
		},
	}
	d.CmdMap["r"] = cmdRoll
	d.CmdMap["rd"] = cmdRoll
	d.CmdMap["roll"] = cmdRoll
	d.CmdMap["rh"] = cmdRoll
	d.CmdMap["rhd"] = cmdRoll
	d.CmdMap["rx"] = cmdRoll
	d.CmdMap["rhx"] = cmdRoll

	cmdExt := &CmdItemInfo{
		Name:  "ext",
		Brief: "查看扩展列表",
		Help:  ".ext // 查看扩展列表",
		Solve: func(ctx *MsgContext, msg *Message, cmdArgs *CmdArgs) CmdExecuteResult {
			if ctx.IsCurGroupBotOn {
				showList := func() {
					text := "检测到以下扩展：\n"
					for index, i := range ctx.Dice.ExtList {
						state := "关"
						for _, j := range ctx.Group.ActivatedExtList {
							if i.Name == j.Name {
								state = "开"
								break
							}
						}
						author := i.Author
						if author == "" {
							author = "<未注明>"
						}
						text += fmt.Sprintf("%d. [%s]%s - 版本:%s 作者:%s\n", index+1, state, i.Name, i.Version, author)
					}
					text += "使用命令: .ext <扩展名> on/off 可以在当前群开启或关闭某扩展。\n"
					text += "命令: .ext <扩展名> 可以查看扩展介绍及帮助"
					ReplyGroup(ctx, msg.GroupId, text)
				}

				if len(cmdArgs.Args) == 0 {
					showList()
				}

				if len(cmdArgs.Args) >= 1 {
					if cmdArgs.IsArgEqual(1, "list") {
						showList()
					} else if cmdArgs.IsArgEqual(2, "on") {
						extName := cmdArgs.Args[0]
						for _, i := range d.ExtList {
							if i.Name == extName {
								ctx.Group.ActivatedExtList = append(ctx.Group.ActivatedExtList, i)
								ReplyGroup(ctx, msg.GroupId, fmt.Sprintf("打开扩展 %s", extName))
								break
							}
						}
					} else if cmdArgs.IsArgEqual(2, "off") {
						extName := cmdArgs.Args[0]
						for index, i := range ctx.Group.ActivatedExtList {
							if i.Name == extName {
								ctx.Group.ActivatedExtList = append(ctx.Group.ActivatedExtList[:index], ctx.Group.ActivatedExtList[index+1:]...)
								ReplyGroup(ctx, msg.GroupId, fmt.Sprintf("关闭扩展 %s", extName))
							}
						}
					} else {
						extName := cmdArgs.Args[0]
						for _, i := range d.ExtList {
							if i.Name == extName {
								text := fmt.Sprintf("> [%s] 版本%s 作者%s\n", i.Name, i.Version, i.Author)
								ReplyToSender(ctx, msg, text+i.GetDescText(i))
								break
							}
						}
					}
				}
			}
			return CmdExecuteResult{true}
		},
	}
	d.CmdMap["ext"] = cmdExt

	cmdNN := &CmdItemInfo{
		Name: "nn <角色名>",
		Help: ".nn <角色名> // 跟角色名则改为指定角色名，不带则重置角色名",
		Solve: func(ctx *MsgContext, msg *Message, cmdArgs *CmdArgs) CmdExecuteResult {
			if msg.MessageType == "group" {
				if ctx.IsCurGroupBotOn {
					if len(cmdArgs.Args) == 0 {
						p := ctx.Player
						p.Name = msg.Sender.Nickname
						VarSetValue(ctx, "$t玩家", &VMValue{VMTypeString, fmt.Sprintf("<%s>", ctx.Player.Name)})

						ReplyGroup(ctx, msg.GroupId, DiceFormatTmpl(ctx, "核心:昵称-重置"))
						//replyGroup(ctx, msg.GroupId, fmt.Sprintf("%s(%d) 的昵称已重置为<%s>", msg.Sender.Nickname, msg.Sender.UserId, p.Name))
					}
					if len(cmdArgs.Args) >= 1 {
						p := ctx.Player
						p.Name = cmdArgs.Args[0]
						VarSetValue(ctx, "$t玩家", &VMValue{VMTypeString, fmt.Sprintf("<%s>", ctx.Player.Name)})

						ReplyGroup(ctx, msg.GroupId, DiceFormatTmpl(ctx, "核心:昵称-改名"))
						//replyGroup(ctx, msg.GroupId, fmt.Sprintf("%s(%d) 的昵称被设定为<%s>", msg.Sender.Nickname, msg.Sender.UserId, p.Name))
					}
				}
			}
			return CmdExecuteResult{true}
		},
	}
	d.CmdMap["nn"] = cmdNN

	cmdSet := &CmdItemInfo{
		Name:  "set <面数>",
		Brief: "设置默认骰子面数，只对自己有效",
		Solve: func(ctx *MsgContext, msg *Message, cmdArgs *CmdArgs) CmdExecuteResult {
			if ctx.IsCurGroupBotOn {
				p := ctx.Player
				if len(cmdArgs.Args) >= 1 {
					num, err := strconv.Atoi(cmdArgs.Args[0])
					if err == nil {
						p.DiceSideNum = num
						ReplyGroup(ctx, msg.GroupId, DiceFormatTmpl(ctx, "核心:设定默认骰子面数"))
						//replyGroup(ctx, msg.GroupId, fmt.Sprintf("设定默认骰子面数为 %d", num))
					} else {
						//replyGroup(ctx, msg.GroupId, fmt.Sprintf("设定默认骰子面数: 格式错误"))
						ReplyGroup(ctx, msg.GroupId, DiceFormatTmpl(ctx, "核心:设定默认骰子面数-错误"))
					}
				} else {
					p.DiceSideNum = 0
					//replyGroup(ctx, msg.GroupId, fmt.Sprintf("重设默认骰子面数为初始"))
					ReplyGroup(ctx, msg.GroupId, DiceFormatTmpl(ctx, "核心:设定默认骰子面数-重置"))
				}
			}
			return CmdExecuteResult{true}
		},
	}
	d.CmdMap["set"] = cmdSet

	cmdText := &CmdItemInfo{
		Name:  "text",
		Brief: "文本指令(测试)，举例: .text 1D16={ 1d16 }，属性计算: 攻击 - 防御 = {攻击} - {防御} = {攻击 - 防御}",
		Help:  ".text <文本模板> // 文本指令，例: .text 看看手气: {1d16}",
		Solve: func(ctx *MsgContext, msg *Message, cmdArgs *CmdArgs) CmdExecuteResult {
			if ctx.IsCurGroupBotOn || ctx.MessageType == "private" {
				val, _, err := d.ExprText(cmdArgs.RawArgs, ctx)

				if err == nil {
					ReplyToSender(ctx, msg, val)
				} else {
					ReplyToSender(ctx, msg, "格式错误")
				}
			}
			return CmdExecuteResult{true}
		},
	}
	d.CmdMap["text"] = cmdText

	cmdChar := &CmdItemInfo{
		Name: "ch",
		//Help: ".ch Save <角色名> // 保存角色，角色名省略则为当前昵称\n.ch load <角色名> // 加载角色\n.ch list // 列出当前角色",
		Help: ".ch list/Save/load/del // 角色管理",
		Solve: func(ctx *MsgContext, msg *Message, cmdArgs *CmdArgs) CmdExecuteResult {
			if ctx.IsCurGroupBotOn {
				getNickname := func() string {
					name, _ := cmdArgs.GetArgN(2)
					if name == "" {
						name = ctx.Player.Name
					}
					return name
				}

				if cmdArgs.IsArgEqual(1, "list") {
					vars := ctx.LoadPlayerVars()
					characters := []string{}
					for k, _ := range vars.ValueMap {
						if strings.HasPrefix(k, "$ch:") {
							characters = append(characters, k[4:])
						}
					}
					if len(characters) == 0 {
						ReplyToSender(ctx, msg, fmt.Sprintf("<%s>当前还没有角色列表", ctx.Player.Name))
					} else {
						ReplyToSender(ctx, msg, fmt.Sprintf("<%s>的角色列表为:\n%s", ctx.Player.Name, strings.Join(characters, "\n")))
					}
				} else if cmdArgs.IsArgEqual(1, "load") {
					name := getNickname()
					vars := ctx.LoadPlayerVars()
					data, exists := vars.ValueMap["$ch:"+name]

					if exists {
						ctx.Player.ValueMap = make(map[string]VMValue)
						err := JsonValueMapUnmarshal([]byte(data.Value.(string)), &ctx.Player.ValueMap)
						if err == nil {
							ctx.Player.Name = name
							VarSetValue(ctx, "$t玩家", &VMValue{VMTypeString, fmt.Sprintf("<%s>", ctx.Player.Name)})

							//replyToSender(ctx, msg, fmt.Sprintf("角色<%s>加载成功，欢迎回来。", Name))
							ReplyGroup(ctx, msg.GroupId, DiceFormatTmpl(ctx, "核心:角色管理-加载成功"))
						} else {
							//replyToSender(ctx, msg, "无法加载角色：序列化失败")
							ReplyToSender(ctx, msg, DiceFormatTmpl(ctx, "核心:角色管理-序列化失败"))
						}
					} else {
						//replyToSender(ctx, msg, "无法加载角色：你所指定的角色不存在")
						ReplyToSender(ctx, msg, DiceFormatTmpl(ctx, "核心:角色管理-角色不存在"))
					}
				} else if cmdArgs.IsArgEqual(1, "Save") {
					name := getNickname()
					vars := ctx.LoadPlayerVars()
					v, err := json.Marshal(ctx.Player.ValueMap)

					if err == nil {
						vars.ValueMap["$ch:"+name] = VMValue{
							VMTypeString,
							string(v),
						}
						VarSetValue(ctx, "$t新角色名", &VMValue{VMTypeString, fmt.Sprintf("<%s>", name)})
						//replyToSender(ctx, msg, fmt.Sprintf("角色<%s>储存成功", Name))
						ReplyToSender(ctx, msg, DiceFormatTmpl(ctx, "核心:角色管理-储存成功"))
					} else {
						//replyToSender(ctx, msg, "无法储存角色：序列化失败")
						ReplyToSender(ctx, msg, DiceFormatTmpl(ctx, "核心:角色管理-序列化失败"))
					}
				} else if cmdArgs.IsArgEqual(1, "del", "rm") {
					name := getNickname()
					vars := ctx.LoadPlayerVars()

					VarSetValue(ctx, "$t新角色名", &VMValue{VMTypeString, fmt.Sprintf("<%s>", name)})
					_, exists := vars.ValueMap["$ch:"+name]
					if exists {
						delete(vars.ValueMap, "$ch:"+name)

						text := DiceFormatTmpl(ctx, "核心:角色管理-删除成功")
						if name == ctx.Player.Name {
							VarSetValue(ctx, "$t新角色名", &VMValue{VMTypeString, fmt.Sprintf("<%s>", msg.Sender.Nickname)})
							text += "\n" + DiceFormatTmpl(ctx, "核心:角色管理-删除成功-当前卡")
						}

						ReplyToSender(ctx, msg, text)
					} else {
						ReplyToSender(ctx, msg, DiceFormatTmpl(ctx, "核心:角色管理-角色不存在"))
					}
				} else {
					help := "角色指令\n"
					help += ".ch Save <角色名> // 保存角色，角色名省略则为当前昵称\n.ch load <角色名> // 加载角色，角色名省略则为当前昵称\n.ch list // 列出当前角色\n.ch del <角色名> // 删除角色"
					ReplyToSender(ctx, msg, help)
				}
			}
			return CmdExecuteResult{true}
		},
	}
	d.CmdMap["角色"] = cmdChar
	d.CmdMap["ch"] = cmdChar
	d.CmdMap["char"] = cmdChar
	d.CmdMap["character"] = cmdChar
}
