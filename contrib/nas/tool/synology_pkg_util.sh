#!/bin/bash
# Copyright (c) 2000-2015 Synology Inc. All rights reserved.

pkg_warn () 
{ 
    local ret=$?;
    echo "Error: $@" 1>&2;
    return $?
}
pkg_log () 
{ 
    local ret=$?;
    echo "$@" 1>&2;
    return $ret
}
pkg_get_string () 
{ 
    local file="$1";
    local sec="$2";
    local key="$3";
    local text="$(sed -n '/^\['$sec'\]/,/^'$key'/s/'$key'.*=[^"]*"\(.*\)"/\1/p' "$file")";
    local product_name_original="_DISKSTATION_";
    local product_name=$(pkg_get_product_name);
    local os_name_original="_OSNAME_";
    local os_name=$(pkg_get_os_name);
    local idx=0;
    shift 3;
    for val in "$@";
    do
        text="${text/\{$idx\}/$val}";
        let idx=1+$idx;
    done;
    echo "$text" | sed -e "s/${product_name_original}/${product_name}/g" | sed -e "s/${os_name_original}/${os_name}/g"
}
pkg_dump_info () 
{ 
    local fields="package version maintainer maintainer_url distributor distributor_url arch exclude_arch model
		adminprotocol adminurl adminport firmware dsmuidir dsmappname checkport allow_altport
		startable helpurl report_url support_center install_reboot install_dep_packages install_conflict_packages install_dep_services
		instuninst_restart_services startstop_restart_services start_dep_services silent_install silent_upgrade silent_uninstall install_type
		checksum package_icon package_icon_120 package_icon_128 package_icon_144 package_icon_256 thirdparty support_conf_folder log_collector
		support_aaprofile auto_upgrade_from offline_install precheckstartstop";
    local f= lan= file= sec= key=;
    for f in $fields;
    do
        if [ -n "${!f}" ]; then
            echo $f=\"${!f}\";
        fi;
    done;
    if [ -e "$UISTRING_PATH" -a "$description_sec" -a "$description_key" ]; then
        sec=$description_sec;
        key=$description_key;
        for lan in $UISTRING_PATH/*;
        do
            lan=$(basename "$lan");
            file="$UISTRING_PATH/$lan/strings";
            if [ -r "$file" ]; then
                echo description_$lan=\"$(pkg_get_string "$file" "$sec" "$key")\";
                if [ "x$lan" = "xenu" ]; then
                    echo description=\"$(pkg_get_string "$file" "$sec" "$key")\";
                fi;
            fi;
        done;
    else
        if [ "x" != "x$description" ]; then
            echo "description=\"${description}\"";
        fi;
    fi;
    if [ -e "$UISTRING_PATH" -a "$displayname_sec" -a "$displayname_key" ]; then
        sec=$displayname_sec;
        key=$displayname_key;
        for lan in $UISTRING_PATH/*;
        do
            lan=$(basename "$lan");
            file="$UISTRING_PATH/$lan/strings";
            if [ -r "$file" ]; then
                echo displayname_$lan=\"$(pkg_get_string "$file" "$sec" "$key")\";
                if [ "x$lan" = "xenu" ]; then
                    echo displayname=\"$(pkg_get_string "$file" "$sec" "$key")\";
                fi;
            fi;
        done;
    else
        if [ "x" != "x$displayname" ]; then
            echo "displayname=\"${displayname}\"";
        fi;
    fi
}
plat_to_unified_plat() {
	local plat="$1"
	local unified_plat=

	case "$plat" in
		x86 | bromolow | cedarview | avoton )
			unified_plat="x86 bromolow cedarview avoton"
			;;
		# alpine and alpine4k use same define.
		alpine )
			unified_plat="alpine"
			;;
		*)
			unified_plat="$plat"
			;;
	esac
	echo "$unified_plat"
}

get_var_from_envmak() {
	return 1
	local var="$1"
	shift
	local envmaks="$@"
	local ret=
	local defaultSearchPath="/env.mak /env32.mak"

	for f in "${envmaks[@]}" $defaultSearchPath; do
		if [ ! -r "$f" ]; then
			continue
		fi

		ret=$(grep "^$var=" "$f" | cut -d= -f2)

		if [ -n "$ret" ]; then
			break
		fi
	done

	if [ -z "$ret" ]; then
		pkg_warn "get_var_from_envmak: can not extract $var from '[$envmaks $defaultSearchPath]'"
		return 1
	else
		echo "$ret"
	fi
}

pkg_get_platform () 
{ 
    local arch=;
    local PLATFORM_ABBR=$(get_var_from_envmak PLATFORM_ABBR "$1" 2> /dev/null) || return 1;
    if [ -n "$PLATFORM_ABBR" ]; then
        case "$PLATFORM_ABBR" in 
            6180)
                arch="88f6180"
            ;;
            6281)
                arch="88f6281"
            ;;
            816x)
                arch="ti816x"
            ;;
            ppc)
                arch="powerpc"
            ;;
            824x)
                arch="ppc824x"
            ;;
            853x)
                arch="ppc853x"
            ;;
            854x)
                arch="ppc854x"
            ;;
            x64)
                arch="x86"
            ;;
            *)
                arch="$PLATFORM_ABBR"
            ;;
        esac;
    fi;
    if [ -z "$arch" ]; then
        local SYNO_PLATFORM=$(get_var_from_envmak SYNO_PLATFORM "$1") || return 1;
        case "$SYNO_PLATFORM" in 
            MARVELL_88F6180)
                arch="88f6180"
            ;;
            MARVELL_88F6281)
                arch="88f6281"
            ;;
            TI_816X)
                arch="ti816x"
            ;;
            POWERPC)
                arch="powerpc"
            ;;
            PPC_824X)
                arch="ppc824x"
            ;;
            PPC_853X)
                arch="ppc853x"
            ;;
            PPC_854X)
                arch="ppc854x"
            ;;
            PPC_QORIQ)
                arch="qoriq"
            ;;
            X64)
                arch="x86"
            ;;
            BROMOLOW)
                arch="bromolow"
            ;;
            CEDARVIEW)
                arch="cedarview"
            ;;
            AVOTON)
                arch="avoton"
            ;;
            MARVELL_ARMADAXP)
                arch="armadaxp"
            ;;
            MARVELL_ARMADA370)
                arch="armada370"
            ;;
            MARVELL_ARMADA375)
                arch="armada375"
            ;;
            EVANSPORT)
                arch="evansport"
            ;;
            PPC_CATALINA)
                arch="catalina"
            ;;
            MINDSPEED_COMCERTO2K)
                arch="comcerto2k"
            ;;
            ALPINE)
                arch="alpine"
            ;;
            BROADCOM_NORTHSTARPLUS)
                arch="northstarplus"
            ;;
            STM_MONACO)
                arch="monaco"
            ;;
            HISILICON_HI3535)
                arch="hi3535"
            ;;
            MARVELL_ARMADA38X)
                arch="armada38x"
            ;;
            *)
                arch=""
            ;;
        esac;
    fi;
    echo "$arch"
}
pkg_get_spk_platform () 
{ 
    local plat=$(pkg_get_platform "$1") || return 1;
    local spk_plat=;
    case "$plat" in 
        88f6281)
            spk_plat="88f628x"
        ;;
        *)
            spk_plat="$plat"
        ;;
    esac;
    echo "$spk_plat"
}
pkg_make_package () 
{ 
    local source_path=$1;
    local dest_path=$2;
    local package_name="package.tgz";
    local temp_extractsize="extractsize_tmp";
    local pkg_size=;
    local tar_option="$(pkg_get_tar_option)";
    if [ -z "$source_path" -o ! -d "$source_path" ]; then
        pkg_warn "pkg_make_package: bad parameters, please set source dir";
        return 1;
    fi;
    if [ -z "$dest_path" -o ! -d "$dest_path" ]; then
        pkg_warn "pkg_make_package: bad parameters, please set destination dir";
        return 1;
    fi;
    pkg_size=`du -sk "$source_path" | awk '{print $1}'`;
    echo "${pkg_size}" >> "$dest_path/$temp_extractsize";
    echo ls $source_path \| tar $tar_option "$dest_path/$package_name" -C "$source_path" -T /dev/stdin;
    ls $source_path | tar $tar_option "$dest_path/$package_name" -C "$source_path" -T /dev/stdin
}
pkg_get_spk_name () 
{ 
    __get_spk_name pkg_get_spk_platform $@
}
pkg_make_spk () 
{ 
    local pack="tar cf";
    local source_path=$1;
    local dest_path=$2;
    local info_path="$source_path/INFO";
    local spk_name=$3;
    local spk_arch=;
    local temp_extractsize="extractsize_tmp";
    if [ -z "$source_path" -o ! -d "$source_path" ]; then
        pkg_warn "pkg_make_spk: bad parameters, please set source dir";
        return 1;
    fi;
    if [ -z "$dest_path" -o ! -d "$dest_path" ]; then
        pkg_warn "pkg_make_spk: bad parameters, please set destination dir";
        return 1;
    fi;
    if [ ! -r "$info_path" ]; then
        pkg_warn "pkg_make_spk: INFO '$info_path' is not existed";
        return 1;
    fi;
    spk_name=${3:-`pkg_get_spk_name $info_path`};
    pkg_size=`cat $source_path/$temp_extractsize`;
    echo "extractsize=\"${pkg_size}\"" >> $info_path;
    rm "$source_path/$temp_extractsize";
    echo "toolkit_version=\"$DSM_BUILD_NUM\"" >> $info_path;
    echo "create_time=\"$(date +%Y%m%d-%T)\"" >> $info_path;
    pkg_log "creating package: $spk_name";
    pkg_log "source:           $source_path";
    pkg_log "destination:      $dest_path/$spk_name";
    $pack "$dest_path/$spk_name" -C "$source_path" $(ls $source_path)
}
pkg_get_unified_platform () 
{ 
    local plat=$(pkg_get_platform "$1") || return 1;
    plat_to_unified_plat "$plat"
}
pkg_get_spk_unified_platform () 
{ 
    local plat=$(pkg_get_platform "$1") || return 1;
    local spk_unified_platform=;
    case "$plat" in 
        88f6281)
            spk_unified_platform="88f628x"
        ;;
        x86 | bromolow | cedarview | avoton)
            spk_unified_platform="x64"
        ;;
        alpine)
            spk_unified_platform="alpine"
        ;;
        *)
            spk_unified_platform="$plat"
        ;;
    esac;
    echo "$spk_unified_platform"
}
pkg_get_spk_unified_name () 
{ 
    __get_spk_name pkg_get_spk_unified_platform $@
}

__get_spk_name() { #<info path>
	local spk_name=
	local platform_func="$1"
	local info_path="${2:-$PKG_DIR/INFO}"
	local package_name="$3"

	. $info_path

	# construct package name
	if [ -z "$package" -o -z "$arch" -o -z "$version" ]; then
		pkg_warn "pkg_make_spk: package, arch, version can not be empty"
		return 1
	fi

	if [ "x$arch" = "xnoarch" ]; then
		spk_arch="noarch"
	elif ! spk_arch=$($platform_func); then
		spk_arch="none"
	fi

	if [ "x$arch" = "xnoarch" ]; then
		spk_arch=""
	else
		spk_arch="-"$spk_arch
	fi

	if [ -z "$package_name" ]; then
		package_name="$package";
	fi

	if [ "${NOSTRIP}" == NOSTRIP ]; then
		spk_name="$package_name$spk_arch-${version}_debug.spk"
	else
		spk_name="$package_name$spk_arch-$version.spk"
	fi
	echo $spk_name;
}


pkg_get_dsm_buildnum() { # [path of VERSION (default: )]
	local version_file=${1:-/source/lnxsdk/init/etc/VERSION}
	local dsm_build=

	if [ ! -r "$version_file" ]; then
		pkg_warn "pkg_get_dsm_buildnum: can not find version file '$version_file'"
		pkg_warn "use default buildnum: 0"
		echo 0
		return 1
	fi

	if ! dsm_build=$(grep -s ^buildnumber "$version_file" | awk -F \" '{print $2}'); then
		echo 0
		return 1
	fi

	echo $(($dsm_build))
}

pkg_get_tar_option() {
	local version_file="/PkgVersion"

	if [ -r $version_file ] && [ "$(pkg_get_dsm_buildnum $version_file)" -ge 5943 ]; then
		echo "cJf"
	else
		echo "czf"
	fi
}

[ "$(caller)" != "0 NULL" ] && return 0
Usage() {
	cat >&2 << EOF
Usage
	$(basename $0) <action> [action options...]
Action
	make_spk <source path> <dest path>
	make_package <source path> <dest path>
EOF
	exit 0
}
[ $# -eq 0 ] && Usage
PkgBuildAction=$1 ; shift
case "$PkgBuildAction" in
	make_spk)	pkg_make_spk "$@" ;;
	make_package)	pkg_make_package "$@" ;;
	*)		Usage ;;
esac
