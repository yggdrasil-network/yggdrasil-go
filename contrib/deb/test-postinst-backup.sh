#!/bin/sh

set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
test_root=$(mktemp -d)
trap 'rm -rf "$test_root"' EXIT HUP INT TERM

mkdir -p "$test_root/bin" "$test_root/etc/yggdrasil" "$test_root/var/backups"

# Run the postinst body against a disposable filesystem, without system services.
awk '
  /^cat > .*\/debian\/postinst << / { in_postinst = 1; next }
  in_postinst && /^EOF$/ { exit }
  in_postinst { print }
' "$repo_root/contrib/deb/generate.sh" |
  sed \
    -e "s#/etc/yggdrasil#$test_root/etc/yggdrasil#g" \
    -e "s#/var/backups#$test_root/var/backups#g" \
    -e "s#/usr/bin/yggdrasil#$test_root/bin/yggdrasil#g" > "$test_root/postinst"

for command_name in systemctl getent chown; do
  printf '#!/bin/sh\nexit 0\n' > "$test_root/bin/$command_name"
  chmod +x "$test_root/bin/$command_name"
done
printf '#!/bin/sh\ncat\n' > "$test_root/bin/yggdrasil"
chmod +x "$test_root/bin/yggdrasil"

printf 'PrivateKey: test-private-key\n' > "$test_root/etc/yggdrasil/yggdrasil.conf"
chmod 644 "$test_root/etc/yggdrasil/yggdrasil.conf"

PATH="$test_root/bin:$PATH" sh "$test_root/postinst"

backup_path="$test_root/var/backups/yggdrasil.conf.$(date +%Y%m%d)"
test -f "$backup_path"
if stat -c %a "$backup_path" >/dev/null 2>&1; then
  backup_mode=$(stat -c %a "$backup_path")
else
  backup_mode=$(stat -f %Lp "$backup_path")
fi
test "$backup_mode" = 600 || {
  echo "Expected private-key backup mode 600, got $backup_mode" >&2
  exit 1
}
cmp -s "$backup_path" "$test_root/etc/yggdrasil/yggdrasil.conf"

# A second install must replace a legacy public backup without retaining its mode.
chmod 644 "$backup_path" "$test_root/etc/yggdrasil/yggdrasil.conf"
PATH="$test_root/bin:$PATH" sh "$test_root/postinst"
if stat -c %a "$backup_path" >/dev/null 2>&1; then
  backup_mode=$(stat -c %a "$backup_path")
else
  backup_mode=$(stat -f %Lp "$backup_path")
fi
test "$backup_mode" = 600 || {
  echo "Expected existing backup to be replaced with mode 600, got $backup_mode" >&2
  exit 1
}
