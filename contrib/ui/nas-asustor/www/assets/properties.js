var ed = {
  partnerId: 1422,
  applicationName: 'RiV Mesh Asustor ADM App',
  nasOSName: 'Asustor ADM',
  useAuthNASRichScreen: true,
  nasVisitEDWebsiteLogin: "https://github.com/RiV-chain/RiV-mesh",
  nasVisitEDWebsiteSignup: "https://github.com/RiV-chain/RiV-mesh",
  nasVisitEDWebsiteLoggedin: "https://github.com/RiV-chain/RiV-mesh",
	getNasAuthUrl: function () {
		return "/";
	},
	nasLoginCall: function (nasLoginSuccess, nasLoginFailure) {
		var d = new Date();
		d.setTime(d.getTime() + (10 * 60 * 1000));
		document.cookie = "access_key=" + btoa( "user=" + encodeURIComponent($('#nasInputUser').val()) + ";pwd=" + encodeURIComponent($('#nasInputPassword').val()))+ "; expires=" + d.toUTCString() + "; path=/";
		$.ajax({url: "api/self"}).done(function () {
			window.location.reload();
		}).fail(function () {
			ed.nasLogoutCall();
			nasLoginFailure();
		});
	},
	nasLogoutCall: function () {
		document.cookie = "access_key=; expires=Thu, 01 Jan 1970 00:00:00 UTC; path=/";
	},
	getNasUser: function() {
		function getCookie(name) {
			var matches = document.cookie.match(new RegExp(
				"(?:^|; )" + name.replace(/([\.$?*|{}\(\)\[\]\\\/\+^])/g, '\\$1') + "=([^;]*)"
			));
			return matches ? decodeURIComponent(matches[1]) : undefined;
		}
		return decodeURIComponent(atob(getCookie('access_key')).split(';')[0].split('=')[1]);
	}
};
