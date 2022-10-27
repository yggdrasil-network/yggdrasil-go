var ed = {
	partnerId: 1222,
	brand: 'RiV Mesh',
	applicationName: "RiV Mesh QNAP NAS OS App",
	nasOSName: "QNAP NAS device",
	useAuthNASRichScreen: true,
	nasVisitEDWebsiteLogin: "https://github.com/RiV-chain/RiV-mesh",
	nasVisitEDWebsiteSignup: "https://github.com/RiV-chain/RiV-mesh",
	nasVisitEDWebsiteLoggedin: "https://github.com/RiV-chain/RiV-mesh",
	getNasAuthUrl: function () {
		return "/";
	}
};

$(function () {

	ed.nasLoginCall = function (nasLoginSuccess, nasLoginFailure) {
		/* encode function start */
		var ezEncodeChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

		function utf16to8(str)
		{
			var out, i, len, c;
			out = "";
			len = str.length;
			for (i = 0; i < len; i++) {
				c = str.charCodeAt(i);
				if ((c >= 0x0001) && (c <= 0x007F)) {
					out += str.charAt(i);
				} else if (c > 0x07FF) {
					out += String.fromCharCode(0xE0 | ((c >> 12) & 0x0F));
					out += String.fromCharCode(0x80 | ((c >> 6) & 0x3F));
					out += String.fromCharCode(0x80 | ((c >> 0) & 0x3F));

				} else {
					out += String.fromCharCode(0xC0 | ((c >> 6) & 0x1F));
					out += String.fromCharCode(0x80 | ((c >> 0) & 0x3F));
				}
			}
			return out;
		}

		function ezEncode(str)
		{
			var out, i, len;
			var c1, c2, c3;

			len = str.length;
			i = 0;
			out = "";
			while (i < len)
			{
				c1 = str.charCodeAt(i++) & 0xff;
				if (i == len)
				{
					out += ezEncodeChars.charAt(c1 >> 2);
					out += ezEncodeChars.charAt((c1 & 0x3) << 4);
					out += "==";
					break;
				}
				c2 = str.charCodeAt(i++);
				if (i == len)
				{
					out += ezEncodeChars.charAt(c1 >> 2);
					out += ezEncodeChars.charAt(((c1 & 0x3) << 4) | ((c2 & 0xF0) >> 4));
					out += ezEncodeChars.charAt((c2 & 0xF) << 2);
					out += "=";
					break;
				}
				c3 = str.charCodeAt(i++);
				out += ezEncodeChars.charAt(c1 >> 2);
				out += ezEncodeChars.charAt(((c1 & 0x3) << 4) | ((c2 & 0xF0) >> 4));
				out += ezEncodeChars.charAt(((c2 & 0xF) << 2) | ((c3 & 0xC0) >> 6));
				out += ezEncodeChars.charAt(c3 & 0x3F);
			}
			return out;
		}

		var d = new Date();
		d.setTime(d.getTime() + (30 * 60 * 1000));
		document.cookie = "qnapuser=" + encodeURIComponent($('#nasInputUser').val()) + "; expires=" + d.toUTCString() + "; path=/";
		document.cookie = "qnappwd=" + encodeURIComponent(ezEncode(utf16to8($('#nasInputPassword').val()))) + "; expires=" + d.toUTCString() + "; path=/";
		$.ajax({url: "rest/info"}).done(function (response) {
			window.location.reload();
			checkError(response);
		}).fail(function () {
			ed.nasLogoutCall();
			nasLoginFailure();
		});
	};
	ed.nasLogoutCall = function() {
		document.cookie = "qnapuser=; expires=Thu, 01 Jan 1970 00:00:00 UTC; path=/";
		document.cookie = "qnappwd=; expires=Thu, 01 Jan 1970 00:00:00 UTC; path=/";
	};
	function getCookie(name) {
		var matches = document.cookie.match(new RegExp(
			"(?:^|; )" + name.replace(/([\.$?*|{}\(\)\[\]\\\/\+^])/g, '\\$1') + "=([^;]*)"
		));
		return matches ? decodeURIComponent(matches[1]) : undefined;
	}
	ed.getNasUser = function() {
		return getCookie('qnapuser');
	};
});
