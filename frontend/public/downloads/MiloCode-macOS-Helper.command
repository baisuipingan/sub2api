#!/bin/bash
# MiloCode macOS installation helper. No downloads or global security changes.
# Run after extracting MiloCode into /Applications or ~/Applications:
#   bash ~/Downloads/MiloCode-macOS-Helper.command

set -euo pipefail

fail() {
  printf '\n%s\n' "$1" >&2
  exit 1
}

[[ $# -eq 0 ]] || fail '此脚本不接受参数。请先将 MiloCode 移入「应用程序」再运行。'
[[ "$(/usr/bin/uname -s)" == Darwin ]] || fail '此安装辅助脚本仅适用于 macOS。'

system_app='/Applications/MiloCode.app'
user_app="${HOME}/Applications/MiloCode.app"
app=''

if [[ -e "$system_app" || -L "$system_app" ]]; then
  app="$system_app"
fi
if [[ -e "$user_app" || -L "$user_app" ]]; then
  if [[ -n "$app" ]]; then
    printf '发现两份 MiloCode，请选择要处理的应用：\n1. %s\n2. %s\n' "$system_app" "$user_app"
    printf '输入 1 或 2（其他输入取消）：'
    IFS= read -r choice || exit 0
    case "$choice" in
      1) app="$system_app" ;;
      2) app="$user_app" ;;
      *) printf '已取消，没有修改应用。\n'; exit 0 ;;
    esac
  else
    app="$user_app"
  fi
fi

[[ -n "$app" ]] || fail '没有找到 MiloCode.app。请先解压下载的 ZIP，将 MiloCode 移入「应用程序」，然后重新运行此脚本。'
[[ -d "$app" && ! -L "$app" ]] || fail '应用路径异常（不是普通应用目录或使用了符号链接），未作修改。'
parent="${app%/*}"
physical_parent="$(cd "$parent" && /bin/pwd -P)" || fail '无法读取应用目录。'
[[ "$physical_parent" == "$parent" ]] || fail '应用的上级目录使用了符号链接，未作修改。请将应用直接放入「应用程序」。'

plist="$app/Contents/Info.plist"
[[ -f "$plist" && ! -L "$plist" && ! -L "$app/Contents" ]] || fail '应用信息文件缺失或路径异常，请重新下载安装包。'
identifier="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$plist" 2>/dev/null)" || fail '无法读取应用标识，请重新下载安装包。'
executable="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$plist" 2>/dev/null)" || fail '无法读取应用启动程序，请重新下载安装包。'
[[ "$identifier" == 'app.milo.code' && "$executable" == 'MiloCode' ]] || fail '此应用不是预期的 MiloCode，未作修改。'
[[ -x "$app/Contents/MacOS/MiloCode" && ! -L "$app/Contents/MacOS" && ! -L "$app/Contents/MacOS/MiloCode" ]] || fail '应用启动程序缺失或路径异常，请重新下载安装包。'

printf '\n正在检查 MiloCode 应用完整性……\n'
/usr/bin/codesign --verify --deep --strict "$app" || fail '应用完整性检查未通过，未解除隔离。请从 MiloRouter 下载页重新下载安装包，不要重新签名或关闭系统安全检查。'

printf '\n将处理：%s\n' "$app"
printf 'MiloCode 尚未进行 Apple 开发者签名及公证。\n'
printf '此脚本只移除该应用的下载隔离标记并打开应用，不修改系统安全设置。\n'
printf '完整性检查不代表 Apple 已认证发布者；请仅在确认应用来自 MiloRouter 下载页时继续。\n'
printf '是否继续 / Continue? [y/N] '
IFS= read -r answer || exit 0
case "$answer" in
  y|Y|yes|YES) ;;
  *) printf '已取消，没有修改应用。\n'; exit 0 ;;
esac

/usr/bin/xattr -drs com.apple.quarantine "$app" || fail '未能完整移除隔离标记（可能没有写入权限）。请通过「系统设置 → 隐私与安全性 → 仍要打开」启动，或重新安装到自己的「应用程序」目录。'
/usr/bin/open "$app" || fail '已移除隔离标记，但未能启动。请在「应用程序」中手动打开 MiloCode。'
printf '\n已完成，请查看 MiloCode 窗口。\n'
