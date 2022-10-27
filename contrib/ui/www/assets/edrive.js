$(function () {
	var ed = 'ed' in window ? window.ed : {};
	var info = {
		devicename : null,
		vaultUrl : "https://github.com/RiV-chain/RiV-mesh"
	};
	var basicEDWebsite = "https://github.com/RiV-chain/RiV-mesh";
	var authNASScreen = '#authNASScreen';
	var isLoggedInMode = false;
	
	$.ajaxSetup({cache: false});

	var fillAjax = function (url, ajax) {
		ajax.url = "api/" + url;
		ajax.contentType = 'application/json';
		if ('fillAjax' in ed) {
			ed.fillAjax(ajax);
		}
		return ajax;
	};

	var loadingMode = function (message) {
		$('#signupScreen').hide();
		$('#statusScreen').hide();
		$('#loginScreen').hide();
		$(authNASScreen).hide();
		$('#loadingMessage').text(message);
		$('#loadingScreen').show();
	};

	var signupMode = function () {
		$('#signupScreen').show();
		$('#loadingScreen').hide();
		$('#statusScreen').hide();
		$('#loginScreen').hide();
		$('#signupAlert').hide();
		$('#inputPeerIPS').val('');
		$('#inputPeerPortS').val('');
		$('#inputPortConfirm').val('');
	};

	var statusMode = function () {
		$('#signupScreen').hide();
		$('#loadingScreen').hide();
		$('#statusScreen').show();
		$('#loginScreen').hide();
	};

	var errorHandler = function (httpObj) {
		if (httpObj.status > 300) {
			$('#loadingScreen').hide();
			if (httpObj.status === 401) {
				$('#signupScreen').hide();
				$('#statusScreen').hide();
				$('#loginScreen').hide();
				$('#loginToYourNAS').prop("href", ed.getNasAuthUrl());
				$(authNASScreen).show();
				$("#nasLogoutBlock").hide();
			} else {
				console.error(httpObj);
			}
		}
	};

	var loginMode = function () {
		$('#loadingScreen').hide();
		$('#statusScreen').hide();
		$('#signupScreen').hide();
		$('#loginScreen').show();
	};

	var infoErrorCheckerScheduled = false;
	
	var updateInfo = function (response) {
		info = {};
		for (var key in response) {
			info[key] = response[key];
		}
		for (var key in ed) {
			info[key] = ed[key];
		}
	};

	var checkInfoError = function (response, forceSchedule) {
		forceSchedule = typeof (forceSchedule) !== "undefined" ? forceSchedule : true;
		if(isLoggedInMode) {
			if (response.isAuthenticated === false) {
				$('#statusAlert').html('Login failed: ' + response.authStatus);
				$('#statusAlert').addClass("nas-apps-config-form-message-error");
				$('#statusAlert').show();
			} else {
				$('#statusAlert').hide();
			}
		}
		if (response.appStatus === 'error') {
			$('#applicationAlert').html(response.errorMessage + '<br><br>Please resolve error before continue');
			$('#applicationAlert').show();
			$('#loginBtn').prop('disabled', true);
			$('#signupBtn').prop('disabled', true);
			$('#inputPeerIP').prop('disabled', true);
			$('#inputPeerPort').prop('disabled', true);
			$('#inputPeerIPS').prop('disabled', true);
			$('#inputPeerPortS').prop('disabled', true);
			$('#inputPortConfirm').prop('disabled', true);
		} else {
			$('#applicationAlert').hide();
			$('#loginBtn').prop('disabled', false);
			$('#signupBtn').prop('disabled', false);
			$('#inputPeerIP').prop('disabled', false);
			$('#inputPeerPort').prop('disabled', false);
			$('#inputPeerIPS').prop('disabled', false);
			$('#inputPeerPortS').prop('disabled', false);
			$('#inputPortConfirm').prop('disabled', false);
		}
		if(infoErrorCheckerScheduled !== true || forceSchedule === true) {
			$.ajax(fillAjax('getself', {})).done(function (response) {
				updateInfo(response);
				setTimeout(function(){checkInfoError(response, true);}, 1000);
			});
			infoErrorCheckerScheduled = true;
		}
	};
	
	var startMode = function () {
		$('#loginAlert').hide();
		$('#inputPeerIP').val('');
		$('#inputPeerPort').val('');
		$.ajax(fillAjax('getself', {})).done(function (response) {
			if (ed.useAuthNASRichScreen) {
				$("#nasLogoutBlock").show();
				$('.nas-user-name').text(ed.getNasUser());
			}
			loginMode();
			if (response.size !== 0) {
				isLoggedInMode = true;
				statusMode();
			}
			var self = response;
			checkInfoError(response);
			$('#version').html((self.build_version !== null ? self.build_version : '&nbsp;') + (self.build_name !== null ? (' (' + self.build_name + ')') : ''));
			$('.nas-apps-config-form-app-version').show();
			$('#username').text(self.address);
			updateInfo(response);
	}).fail(errorHandler);
		$('#nasLoginAlert').hide();
		$('#nasInputPassword').val('');
	};

	var waitForEvent = function (eventType, onSuccess, onError, timeout, startTime) {
		startTime = typeof (startTime) === "undefined" ? new Date() : startTime;
		timeout = typeof (timeout) === "undefined" ? Infinity : timeout;
		setTimeout(function () {
			if (((new Date()) - startTime) > timeout) {
				onError("timeout");
			} else {
				$.ajax(fillAjax('events', {
					method: 'GET'
				})).done(function (response) {
					for (var k in response) {
						if (response[k].sourceType === eventType) {
							$.ajax(fillAjax('event/' + k, {
								method: 'DELETE'
							}));
							if (response[k].type === 'success')
								onSuccess();
							else if (response[k].type === 'error')
								onError(response[k].errorMessage);
							return;
						}
					}
					waitForEvent(eventType, onSuccess, onError, timeout, startTime);
				}).fail(function (httpObj) {
					if (httpObj.status === 0) {
						waitForEvent(eventType, onSuccess, onError, timeout, startTime);
					} else {
						errorHandler(httpObj);
					}
				});
			}
		}, 500);
	};

	var loginDoneHandler = function () {
		function onSuccess() {
			//Show logged in screen
			$('#username').text(($('#inputPeerIPS').val() === "") ? $('#inputPeerIP').val() : $('#inputPeerIPS').val());
			statusMode();
			$('#statusAlert').html($('#loginSuccess').html());
			$('#statusAlert').removeClass("nas-apps-config-form-message-error");
			$('#statusAlert').show();
		}
		function onError(message) {
			if (message === "timeout") {
				$.ajax(fillAjax('info', {})).done(function (response) {
					if (response.isAuthenticated === false) {
						waitForEvent("login", onSuccess, onError, 30000);
					} else {
						onSuccess();
					}
				}).fail(errorHandler);
			} else {
				if (message === "PEER_UNAVAILABLE" || message === "DN_UNRESOLVED") {
					message = "Unable to connect to a peer";
				}
				$('#loginAlert').html(message);
				$('#loginAlert').show();
				loginMode();
			}
		}
		waitForEvent("login", onSuccess, onError, 30000);
	};

	$('#loginBtn').click(function (e) {
		e.preventDefault();
		if ($('#loadingScreen').is(":visible")) return;
		$('#inputPeerIP').val($('#inputPeerIP').val().trim());
		if ($('#inputPeerIP').val().length === 0 || $('#inputPeerPort').val().trim().length === 0) {
			return;
		}
		$('#loginAlert').hide();
		loadingMode($('#loginLoadingMessage').text());

		$.ajax(fillAjax('user', {
			method: 'POST',
			data: JSON.stringify({user: $('#inputPeerIP').val(), pass: $('#inputPeerPort').val()})
		})).done(loginDoneHandler).statusCode({202: loginDoneHandler}).fail(errorHandler);
	});


	$("#inputPeerIP, #inputPeerPort").keypress(function (e) {
		if (e.which === 13) {
			$("#loginBtn").trigger("click");
		}
	});

	$('#logoutBtn').click(function (e) {
		e.preventDefault();
		if ($('#loadingScreen').is(":visible")) return;

		loadingMode($('#logoutLoadingMessage').text());
		var doneHandler = function () {
			$('#inputPeerIP').val('');
			$('#inputPeerPort').val('');
			setTimeout(function () {
				location.reload();
			}, 5000);
		};
		$.ajax(fillAjax('user', {
			method: 'DELETE'
		})).done(doneHandler).statusCode({202: doneHandler}).fail(errorHandler);
	});

	$('#manageBackups').click(function (e) {
		e.preventDefault();
		$.ajax(fillAjax('autologin', {
			async: false
		})).done(function (response) {
			var params = response.autologinToken;
			params["tab"] = "naswizard";
			params["dname"] = info.devicename;
			window.open(info.vaultUrl + "/account/login.aspx?" + $.param(params), "_blank");
		}).fail(errorHandler);
	});

	var currentLocation = function () {
		var url = location.href;
		// http://xxx/<path>/  ->  http://xxx/<path>/
		if (url.slice(-1) === "/") {
			return url;
			// http://xxx/<path>/index.html ->  http://xxx/<path>/
		} else if (url.lastIndexOf("/") < url.lastIndexOf(".")) {
			return url.substring(0, url.lastIndexOf("/") + 1);
			// http://xxx/<path>  ->  http://xxx/<path>/
		} else {
			return url + "/";
		}
	};

	$('#logFileBtn').click(function (e) {
		e.preventDefault();
		window.open(currentLocation().split("#")[0] + 'log', "_blank");
	});

	$('#getDiagFileBtn').click(function (e) {
		e.preventDefault();
		window.open(currentLocation().split("#")[0] + 'diagnostics.zip');
	});

	$('#checkPeerBtn').click(function (e) {
		e.preventDefault();
		window.open(info.vaultUrl + "/account/check_peer?UName=" + encodeURIComponent($('#inputPeerIP').val().trim()), "_blank");
	});

	$('#goToSignupBtn').click(function (e) {
		e.preventDefault();
		signupMode();
	});

	$('#signupBackToLoginBtn').click(function (e) {
		e.preventDefault();
		startMode();
	});

	var signupDoneHandler = function () {
		waitForEvent("register", loginDoneHandler, function (message) {
			$('#loadingScreen').hide();
			$('#signupScreen').show();
			$('#signupAlert').html(message);
			$('#signupAlert').show();
		});
	};

	$('#signupBtn').click(function (e) {
		e.preventDefault();
		if ($('#loadingScreen').is(":visible")) return;

		if ($('#inputPeerPortS').val() !== $('#inputPortConfirm').val()) {
			$('#signupAlert').text($('#hostFormatError').text());
			$('#signupAlert').show();
			return;
		}

		$('#inputPeerIPS').val($('#inputPeerIPS').val().trim());
		loadingMode($('#signupLoadingMessage').text());

		$.ajax(fillAjax('user', {
			method: 'PUT',
			data: JSON.stringify({user: $('#inputPeerIPS').val(), pass: $('#inputPeerPortS').val(), c: ed.partnerId})
		})).done(signupDoneHandler).statusCode({202: signupDoneHandler}).fail(errorHandler);
	}); //signupBtn.click

	$("#inputPeerIPS, #inputPeerPortS, #inputPortConfirm").keypress(function (e) {
		if (e.which === 13) {
			$("#signupBtn").trigger("click");
		}
	});

	var nasLoginSuccess = function () {
		$('.nas-user-name').text(ed.getNasUser());
	};

	var nasLoginFailure = function (message) {
		//Show notification: IP/DN errors
		if (typeof message !== 'undefined') {
			$('#nasLoginAlert').html(message);
		} else {
			$('#nasLoginAlert').text($('#ipOrDNError').text());
		}
		$(authNASScreen).show();
		$('#nasInputPassword').val('');
		$('#nasLoginAlert').show();
		$('#loadingScreen').hide();
	};

	$('#nasLoginBtn').click(function (e) {
		e.preventDefault();
		$('#nasInputUser').val($('#nasInputUser').val().trim());
		if ($('#nasInputUser').val().length === 0 || $('#nasInputPassword').val().trim().length === 0) {
			return;
		}
		$('#nasLoginAlert').hide();
		loadingMode($('#loginLoadingMessage').text());
		ed.nasLoginCall(nasLoginSuccess, nasLoginFailure);
	});


	$("#nasInputUser, #nasInputPassword").keypress(function (e) {
		if (e.which === 13) {
			$("#nasLoginBtn").trigger("click");
			return false;
		}
	});

	$('#nasLogoutBtn').click(function (e) {
		e.preventDefault();
		ed.nasLogoutCall();
		$("#nasLogoutBlock").hide();
		window.location.reload();
	});

	var main = function () {
		document.getElementById("dynamic_body").style.display = "block";
		$('title').text(ed.applicationName);
		$('.nas-os-name').text(ed.nasOSName);
		if ('nasVisitEDWebsiteLogin' in ed) {
			$('.nas-visit-ed-website-login').prop("href", ed.nasVisitEDWebsiteLogin);
		} else {
			$('.nas-visit-ed-website-login').prop("href", basicEDWebsite);
		}
		if ('nasVisitEDWebsiteSignup' in ed) {
			$('.nas-visit-ed-website-signup').prop("href", ed.nasVisitEDWebsiteSignup);
		} else {
			$('.nas-visit-ed-website-signup').prop("href", basicEDWebsite);
		}
		if ('nasVisitEDWebsiteLoggedin' in ed) {
			$('.nas-visit-ed-website-loggedin').prop("href", ed.nasVisitEDWebsiteLoggedin);
		} else {
			$('.nas-visit-ed-website-loggedin').prop("href", basicEDWebsite);
		}
		if ('signingUpMessageHtml' in ed) {
			$('#signingUpMessage').html(ed.signingUpMessageHtml);
		}
		if ('signingUpPropositionMessageHtml' in ed) {
			$('#signingUpPropositionMessage').html(ed.signingUpPropositionMessageHtml);
		}
		if ('useAuthNASRichScreen' in ed && ed.useAuthNASRichScreen) {
			authNASScreen = '#authNASRichScreen';
		} else {
			ed.useAuthNASRichScreen = false;
		}
		startMode();
	};

	main();

});
