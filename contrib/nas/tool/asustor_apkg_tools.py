#!/usr/bin/env python2
# Copyright (c) 2011-2013 Asustor Systems, Inc. All Rights Reserved.

# -*- coding: utf-8 -*-

import os
import sys
import argparse
import zipfile
import tarfile
import tempfile
import shutil
import json
import glob
import re
import csv

__author__    = 'Walker Lee <walkerlee@asustor.com>'
__copyright__ = 'Copyright (C) 2011-2013  ASUSTOR Systems, Inc.  All Rights Reserved.'
__version__   = '0.1'
__abs_path__  = os.path.abspath(sys.argv[0])
__program__   = os.path.basename(__abs_path__)

def find_developer(app):
	developer = None

	if os.path.exists('apkg-developer-mapping.csv'):
		with open('apkg-developer-mapping.csv', 'r') as f:
			for row in csv.reader(f):
				if row[0] == app:
					developer = row[1]
					break;

	return developer

class Chdir:
	def __init__(self, newPath):
		self.newPath = newPath

	def __enter__(self):
		self.savedPath = os.getcwd()
		os.chdir(self.newPath)

	def __exit__(self, etype, value, traceback):
		os.chdir(self.savedPath)

class Apkg:
	umask   = 0022
	tmp_dir = '/tmp'

	tmp_prefix = 'APKG-'

	apk_format = {
		'version' : '2.0',
		'format'  : 'zip',
		'suffix'  : 'apk'
	}

	apk_file_contents = {
		'version' : 'apkg-version',
		'data'    : 'data.tar.gz',
		'control' : 'control.tar.gz'
	}

	apk_special_folders = {
		'control' : 'CONTROL',
		'webman'  : 'webman',
		'web'     : 'www'
	}

	apk_control_files = {
		'pkg-config'            : 'config.json',
		'changlog'              : 'changelog.txt',
		'description'           : 'description.txt',
		'icon'                  : 'icon.png',
		'script-pre-install'    : 'pre-install.sh',
		'script-pre-uninstall'  : 'pre-uninstall.sh',
		'script-post-install'   : 'post-install.sh',
		'script-post-uninstall' : 'post-uninstall.sh',
		'script-start-stop'     : 'start-stop.sh',
	}

	apk_web_settings = {
		'user'  : 'admin',
		'group' : 'administrators',
		'uid'   : 999,
		'gid'   : 999,
		'perms' : 0770
	}

	def __init__(self):
		self.pid = os.getpid()
		self.cwd = os.getcwd()
		self.pkg_tmp_dir = self.tmp_dir + '/APKG.' + str(self.pid)

	def __del__(self):
		pass

	def pkg_misc_check(self):
		pass

	def compress_pkg(self):
		pass

	def __check_apk_format(self, apk_file):
		file_list = []

		# check apk file format
		try:
			with zipfile.ZipFile(apk_file, 'r') as apk_zip:
				file_list = apk_zip.namelist()
		except zipfile.BadZipfile:
			print 'File is not a apk file: %s' % (apk_file)
			return False

		# check apk file contents
		if not file_list:
			print 'File is empty: %s' % (apk_file)
			return False

		result = True
		for (key, value) in self.apk_file_contents.items():
			if value not in file_list:
				print 'Can\'t found file in apk file: %s' % (value)
				result = False

		return result

	# return True for files we want to exclude
	def __excluded_files(self, file):
		_return = False
		# here we're checking to see if the file is 'CONTROL' -
		# a file don't want included in our tar archive.
		if file.find('CONTROL') > -1:
			_return = True
		return _return

	def __zip_archive(self, apk_file, file_list):
		with zipfile.ZipFile(apk_file, 'w') as apk_zip:
			for one_file in file_list:
				apk_zip.write(one_file)

	def __zip_extract(self, apk_file, member, path):
		with zipfile.ZipFile(apk_file, 'r') as apk_zip:
			apk_zip.extract(member, path)

	def __tar_archive(self, tar_file, path):
		# create a tar archive of directory
		with tarfile.open(tar_file, 'w:gz') as tar:
			if os.path.basename(tar_file) == self.apk_file_contents['data']:
				tar.add(path, exclude=self.__excluded_files)
			else:
				tar.add(path)

	def __tar_extract(self, tar_file, path):
		with tarfile.open(tar_file, 'r:gz') as tar:
			tar.extractall(path)

	def __get_apkg_version(self, version_file):
		with file(version_file) as f:
			version = f.read().rstrip()
		return version

	def __get_app_info_v1(self, control_dir):
		with open(control_dir + '/' + self.apk_control_files['pkg-config']) as data_file:
			data = json.load(data_file)
		return data

	def __get_app_info_v2(self, control_dir):
		with open(control_dir + '/' + self.apk_control_files['pkg-config']) as data_file:
			data = json.load(data_file)
		return data

	def __get_app_info(self, control_dir, apkg_version):
		if apkg_version == '1.0':
			return self.__get_app_info_v1(control_dir)
		elif apkg_version == '2.0':
			return self.__get_app_info_v2(control_dir)
		else:
			return None

	def __check_app_layout(self, app_dir):
		control_dir = app_dir + '/' + self.apk_special_folders['control']

		if not os.path.isdir(control_dir):
			print '[Not found] CONTROL folder: %s' % (control_dir)
			return False

		config_file = control_dir + '/' + self.apk_control_files['pkg-config']

		if not os.path.isfile(config_file):
			print '[Not found] config file: %s' % (config_file)
			return False

		# TODO: check icon exist?
		icon_file = control_dir + '/' + self.apk_control_files['icon']

		pass

		return True

	def __check_app_info_fields(self, app_info):
		require_fields = ['package', 'version', 'architecture', 'firmware']

		for field in require_fields:
			try:
				if app_info['general'][field].strip() == '':
					print 'Empty field: %s' % (field)
					return False
			except KeyError:
				print 'Missing field: %s' % (field)
				return False

		return True

	def __filter_special_chars(self, string, pattern):
		filter_string = re.sub(pattern, '', string)
		return filter_string

	def __check_app_package_name(self, package):
		return True if self.__filter_special_chars(package, '[a-zA-Z0-9.+-]') == '' else False

	def create(self, folder, dest_dir=None):
		# check folder is exist
		app_dir = os.path.abspath(folder)
		if not os.path.isdir(app_dir):
			print 'Not a directory: %s' % (app_dir)
			return -1

		control_dir = app_dir + '/' + self.apk_special_folders['control']
		config_file = control_dir + '/' + self.apk_control_files['pkg-config']

		# check package layout is correct
		if not self.__check_app_layout(app_dir):
			print 'Invalid App layout: %s' % (app_dir)
			return -1

		# change file mode and owner
		os.chmod(control_dir, 0755)
		os.chown(control_dir, 0, 0)

		all_files = glob.glob(control_dir + '/*')
		sh_files  = glob.glob(control_dir + '/*.sh')
		py_files  = glob.glob(control_dir + '/*.py')

		for one_file in all_files:
			os.chmod(one_file, 0644)
			os.chown(one_file, 0, 0)

		for one_file in sh_files:
			os.chmod(one_file, 0755)
			os.system('dos2unix %s > /dev/null 2>&1' % (one_file))

		for one_file in py_files:
			os.chmod(one_file, 0755)

		app_info = self.__get_app_info(control_dir, self.apk_format['version'])

		# check config.json fields
		if not self.__check_app_info_fields(app_info):
			print 'Invalid App config: %s' % (config_file)
			return -1

		# check package field value
		if not self.__check_app_package_name(app_info['general']['package']):
			print 'Invalid App package field: %s (valid characters [a-zA-Z0-9.+-])' % ('package')
			return -1

		# prepare tmp dir
		tmp_dir = tempfile.mkdtemp(prefix=self.tmp_prefix)

		version_file   = tmp_dir + '/' + self.apk_file_contents['version']
		control_tar_gz = tmp_dir + '/' + self.apk_file_contents['control']
		data_tar_gz    = tmp_dir + '/' + self.apk_file_contents['data']

		if dest_dir == None:
			dest_dir = os.getcwd()
		else:
			dest_dir = os.path.abspath(dest_dir)

		apk_file = dest_dir + '/' + app_info['general']['package'] + '_' + app_info['general']['version'] + '_' + app_info['general']['architecture'] + '.' + self.apk_format['suffix']

		# write apkg version
		with open(version_file, 'w') as apkg_version:
			apkg_version.write(self.apk_format['version'] + '\n')

		# archive data files
		with Chdir(app_dir):
			self.__tar_archive(data_tar_gz, '.')

		# archive control files
		with Chdir(control_dir):
			self.__tar_archive(control_tar_gz, '.')

		# archive apk file
		with Chdir(tmp_dir):
			self.__zip_archive(apk_file, [self.apk_file_contents['version'], self.apk_file_contents['control'], self.apk_file_contents['data']])

		# cleanup temp folder
		shutil.rmtree(tmp_dir, ignore_errors=True)

		return apk_file

	def extract(self, package, dest_dir=None):
		# check file is exist
		apk_file = os.path.abspath(package)
		if not os.path.isfile(apk_file):
			print 'Not a file: %s' % (apk_file)
			return -1

		# check package format (apk: zip format; contain files: apkg-version, control.tar.gz, data.tar.gz)
		if not self.__check_apk_format(apk_file):
			return -1

		# unpack package phase 1
		tmp_dir = tempfile.mkdtemp(prefix=self.tmp_prefix)
		tmp_contents_dir = tmp_dir + '/@contents@'
		os.mkdir(tmp_contents_dir)

		self.__zip_extract(apk_file, self.apk_file_contents['version'], tmp_contents_dir)
		self.__zip_extract(apk_file, self.apk_file_contents['control'], tmp_contents_dir)
		self.__zip_extract(apk_file, self.apk_file_contents['data'], tmp_contents_dir)

		# unpack package phase 2
		tmp_control_dir = tmp_dir + '/' + self.apk_special_folders['control']
		os.mkdir(tmp_control_dir)

		self.__tar_extract(tmp_contents_dir + '/' + self.apk_file_contents['control'], tmp_control_dir)
		self.__tar_extract(tmp_contents_dir + '/' + self.apk_file_contents['data'], tmp_dir)

		# get apkg version
		apkg_version = self.__get_apkg_version(tmp_contents_dir + '/' + self.apk_file_contents['version'])

		# clean tmp contents dir
		shutil.rmtree(tmp_contents_dir, ignore_errors=True)

		# get apk information
		apk_info = self.__get_app_info(tmp_control_dir, apkg_version)

		# error handle
		if apk_info is None:
			print 'Extract error: %s' % (apk_file)
			shutil.rmtree(tmp_dir, ignore_errors=True)
			return -1

		if dest_dir == None:
			dest_dir = os.getcwd()
		else:
			dest_dir = os.path.abspath(dest_dir)

		# move dir
		if apkg_version == '1.0':
			app_dir = dest_dir + '/' + apk_info['app']['name'] + '_' + apk_info['app']['version'] + '_' + apk_info['app']['architecture']
		elif apkg_version == '2.0':
			app_dir = dest_dir + '/' + apk_info['general']['name'] + '_' + apk_info['general']['version'] + '_' + apk_info['general']['architecture']

		if os.path.isdir(app_dir):
			print 'The folder is exist, please remove it: %s' % (app_dir)
			shutil.rmtree(tmp_dir, ignore_errors=True)
			return -1
		else:
			shutil.move(tmp_dir, app_dir)
			return app_dir

	def convert(self, package):
		app_dir = self.extract(package, dest_dir='/tmp')

		if app_dir == -1:
			print 'Convert error'
			return -1

		control_dir      = app_dir + '/' + self.apk_special_folders['control']
		config_file      = control_dir + '/' + self.apk_control_files['pkg-config']
		changelog_file   = control_dir + '/' + self.apk_control_files['changlog']
		description_file = control_dir + '/' + self.apk_control_files['description']

		# get old format app information
		app_old_info = self.__get_app_info(control_dir, self.apk_format['version'])

		app_new_info = {}

		developer = find_developer(app_old_info['app']['package'])

		app_new_info['general'] = {}
		app_new_info['general']['package']      = app_old_info['app']['package']
		app_new_info['general']['name']         = app_old_info['app']['name']
		app_new_info['general']['version']      = app_old_info['app']['version']
		app_new_info['general']['depends']      = app_old_info['app']['depends']
		app_new_info['general']['conflicts']    = app_old_info['app']['conflicts']
		app_new_info['general']['developer']    = app_old_info['app']['website'] if (developer is None) else developer
		app_new_info['general']['maintainer']   = app_old_info['app']['maintainer']
		app_new_info['general']['email']        = app_old_info['app']['email']
		app_new_info['general']['website']      = app_old_info['app']['website']
		app_new_info['general']['architecture'] = app_old_info['app']['architecture']
		app_new_info['general']['firmware']     = '2.0'

		try:
			app_old_info['desktop']
		except KeyError:
			app_old_info['desktop'] = {}

		try:
			app_old_info['desktop']['icon']
		except KeyError:
			app_old_info['desktop']['icon'] = {}

		# remove unused field
		app_old_info['desktop']['icon'].pop('title', None)

		try:
			app_old_info['desktop']['privilege']
		except KeyError:
			app_old_info['desktop']['privilege'] = {}

		app_new_info['adm-desktop'] = {}
		app_new_info['adm-desktop']['app']       = app_old_info['desktop']['icon']
		app_new_info['adm-desktop']['privilege'] = app_old_info['desktop']['privilege']

		try:
			app_old_info['install']['link']
		except KeyError:
			app_old_info['install']['link'] = {}

		try:
			app_old_info['install']['share']
		except KeyError:
			app_old_info['install']['share'] = []

		try:
			app_old_info['install']['service-reg']
		except KeyError:
			app_old_info['install']['service-reg'] = {}

		try:
			app_old_info['install']['service-reg']['priority']
		except KeyError:
			app_old_info['install']['service-reg']['priority'] = {}

		try:
			app_old_info['install']['service-reg']['port']
		except KeyError:
			app_old_info['install']['service-reg']['port'] = []

		try:
			app_old_info['install']['dep-service']
		except KeyError:
			app_old_info['install']['dep-service'] = {}

		try:
			app_old_info['install']['dep-service']['start']
		except KeyError:
			app_old_info['install']['dep-service']['start'] = []

		try:
			app_old_info['install']['dep-service']['restart']
		except KeyError:
			app_old_info['install']['dep-service']['restart'] = []

		app_new_info['register'] = {}
		app_new_info['register']['symbolic-link'] = app_old_info['install']['link']
		app_new_info['register']['share-folder']  = app_old_info['install']['share']
		app_new_info['register']['port']          = app_old_info['install']['service-reg']['port']
		app_new_info['register']['boot-priority'] = {}

		try:
			app_new_info['register']['boot-priority']['start-order'] = app_old_info['install']['service-reg']['priority']['start']
		except KeyError:
			pass

		try:
			app_new_info['register']['boot-priority']['stop-order']  = app_old_info['install']['service-reg']['priority']['stop']
		except KeyError:
			pass

		app_new_info['register']['prerequisites'] = {}
		app_new_info['register']['prerequisites']['enable-service']  = app_old_info['install']['dep-service']['start']
		app_new_info['register']['prerequisites']['restart-service'] = app_old_info['install']['dep-service']['restart']

		# get changelog and description
		changelog   = app_old_info['app'].pop('changes', None).strip()
		description = app_old_info['app'].pop('description', None).strip()

		# convert json object to string
		json_string = json.dumps(app_new_info, indent=3)

		# set new format app information
		with open(config_file, 'w') as new_file:
			new_file.write(json_string + '\n')

		# write changelog.txt
		if changelog is not None and changelog != '':
			with open(changelog_file, 'w') as new_file:
				new_file.write(changelog + '\n')

		# write description.txt
		if description is not None and description != '':
			with open(description_file, 'w') as new_file:
				new_file.write(description + '\n')

		# convert icon
		icon_enable_file  = control_dir + '/icon-enable.png'
		icon_disable_file = control_dir + '/icon-disable.png'
		icon_file         = control_dir + '/' + self.apk_control_files['icon']
		
		os.unlink(icon_disable_file)
		os.rename(icon_enable_file, icon_file)

		convert_dir = os.getcwd() + '/apk-2.0'
		if not os.path.exists(convert_dir):
			os.mkdir(convert_dir)

		# re-pack apk
		apk_file = self.create(app_dir, dest_dir=convert_dir)

		# cleanup app folder
		shutil.rmtree(app_dir, ignore_errors=True)

		print 'Convert success: %s' % (apk_file)

	def upload(self, package):
		# check file is exist
		abs_path = os.path.abspath(package)
		if not os.path.isfile(abs_path):
			print 'Not a file: %s' % (abs_path)
			return -1

		print 'function not support: %s' % ('upload')

# main
if __name__ == "__main__":
	# create the top-level parser
	parser = argparse.ArgumentParser(description='asustor package helper.')

	subparsers = parser.add_subparsers(help='sub-commands')

	# create the parser for the "create" commad
	parser_create = subparsers.add_parser('create', help='create package from folder')
	parser_create.add_argument('folder', help='select a package layout folder to pack')
	parser_create.add_argument('--destination', help='move apk to destination folder')
	parser_create.set_defaults(command='create')

	# create the parser for the "extract" commad
	parser_extract = subparsers.add_parser('extract', help='extract package to folder')
	parser_extract.add_argument('package', help='select a package to extract')
	parser_extract.add_argument('--destination', help='extract apk to destination folder')
	parser_extract.set_defaults(command='extract')

	# create the parser for the "convert" commad
#	parser_convert = subparsers.add_parser('convert', help='convert package format to 2.0')
#	parser_convert.add_argument('package', help='select a package to convert')
#	parser_convert.set_defaults(command='convert')

	# create the parser for the "upload" commad
#	parser_upload = subparsers.add_parser('upload', help='upload package to file server')
#	parser_upload.add_argument('package', help='select a package to upload')
#	parser_upload.set_defaults(command='upload')

	# parsing arguments
	args = parser.parse_args()

	# process commands
	apkg = Apkg()

	if args.command == 'create':
		apkg.create(args.folder, args.destination)

	elif args.command == 'extract':
		apkg.extract(args.package, args.destination)

#	elif args.command == 'convert':
#		apkg.convert(args.package)

#	elif args.command == 'upload':
#		apkg.upload(args.package)
