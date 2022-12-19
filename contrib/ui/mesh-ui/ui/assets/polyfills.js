/**
 * https://github.com/taylorhakes/promise-polyfill
 */
!function(e,t){"object"==typeof exports&&"undefined"!=typeof module?t():"function"==typeof define&&define.amd?define(t):t()}(0,function(){"use strict";function e(e){var t=this.constructor;return this.then(function(n){return t.resolve(e()).then(function(){return n})},function(n){return t.resolve(e()).then(function(){return t.reject(n)})})}function t(e){return new this(function(t,n){function o(e,n){if(n&&("object"==typeof n||"function"==typeof n)){var f=n.then;if("function"==typeof f)return void f.call(n,function(t){o(e,t)},function(n){r[e]={status:"rejected",reason:n},0==--i&&t(r)})}r[e]={status:"fulfilled",value:n},0==--i&&t(r)}if(!e||"undefined"==typeof e.length)return n(new TypeError(typeof e+" "+e+" is not iterable(cannot read property Symbol(Symbol.iterator))"));var r=Array.prototype.slice.call(e);if(0===r.length)return t([]);for(var i=r.length,f=0;r.length>f;f++)o(f,r[f])})}function n(e){return!(!e||"undefined"==typeof e.length)}function o(){}function r(e){if(!(this instanceof r))throw new TypeError("Promises must be constructed via new");if("function"!=typeof e)throw new TypeError("not a function");this._state=0,this._handled=!1,this._value=undefined,this._deferreds=[],l(e,this)}function i(e,t){for(;3===e._state;)e=e._value;0!==e._state?(e._handled=!0,r._immediateFn(function(){var n=1===e._state?t.onFulfilled:t.onRejected;if(null!==n){var o;try{o=n(e._value)}catch(r){return void u(t.promise,r)}f(t.promise,o)}else(1===e._state?f:u)(t.promise,e._value)})):e._deferreds.push(t)}function f(e,t){try{if(t===e)throw new TypeError("A promise cannot be resolved with itself.");if(t&&("object"==typeof t||"function"==typeof t)){var n=t.then;if(t instanceof r)return e._state=3,e._value=t,void c(e);if("function"==typeof n)return void l(function(e,t){return function(){e.apply(t,arguments)}}(n,t),e)}e._state=1,e._value=t,c(e)}catch(o){u(e,o)}}function u(e,t){e._state=2,e._value=t,c(e)}function c(e){2===e._state&&0===e._deferreds.length&&r._immediateFn(function(){e._handled||r._unhandledRejectionFn(e._value)});for(var t=0,n=e._deferreds.length;n>t;t++)i(e,e._deferreds[t]);e._deferreds=null}function l(e,t){var n=!1;try{e(function(e){n||(n=!0,f(t,e))},function(e){n||(n=!0,u(t,e))})}catch(o){if(n)return;n=!0,u(t,o)}}var a=setTimeout;r.prototype["catch"]=function(e){return this.then(null,e)},r.prototype.then=function(e,t){var n=new this.constructor(o);return i(this,new function(e,t,n){this.onFulfilled="function"==typeof e?e:null,this.onRejected="function"==typeof t?t:null,this.promise=n}(e,t,n)),n},r.prototype["finally"]=e,r.all=function(e){return new r(function(t,o){function r(e,n){try{if(n&&("object"==typeof n||"function"==typeof n)){var u=n.then;if("function"==typeof u)return void u.call(n,function(t){r(e,t)},o)}i[e]=n,0==--f&&t(i)}catch(c){o(c)}}if(!n(e))return o(new TypeError("Promise.all accepts an array"));var i=Array.prototype.slice.call(e);if(0===i.length)return t([]);for(var f=i.length,u=0;i.length>u;u++)r(u,i[u])})},r.allSettled=t,r.resolve=function(e){return e&&"object"==typeof e&&e.constructor===r?e:new r(function(t){t(e)})},r.reject=function(e){return new r(function(t,n){n(e)})},r.race=function(e){return new r(function(t,o){if(!n(e))return o(new TypeError("Promise.race accepts an array"));for(var i=0,f=e.length;f>i;i++)r.resolve(e[i]).then(t,o)})},r._immediateFn="function"==typeof setImmediate&&function(e){setImmediate(e)}||function(e){a(e,0)},r._unhandledRejectionFn=function(e){void 0!==console&&console&&console.warn("Possible Unhandled Promise Rejection:",e)};var s=function(){if("undefined"!=typeof self)return self;if("undefined"!=typeof window)return window;if("undefined"!=typeof global)return global;throw Error("unable to locate global object")}();"function"!=typeof s.Promise?s.Promise=r:(s.Promise.prototype["finally"]||(s.Promise.prototype["finally"]=e),s.Promise.allSettled||(s.Promise.allSettled=t))});
/** @license
 * eventsource.js
 * Available under MIT License (MIT)
 * https://github.com/Yaffle/EventSource/
 */
!function(e){"use strict";var r,H=e.setTimeout,N=e.clearTimeout,j=e.XMLHttpRequest,o=e.XDomainRequest,t=e.ActiveXObject,n=e.EventSource,i=e.document,w=e.Promise,d=e.fetch,a=e.Response,h=e.TextDecoder,s=e.TextEncoder,p=e.AbortController;function c(){this.bitsNeeded=0,this.codePoint=0}"undefined"==typeof window||void 0===i||"readyState"in i||null!=i.body||(i.readyState="loading",window.addEventListener("load",function(e){i.readyState="complete"},!1)),null==j&&null!=t&&(j=function(){return new t("Microsoft.XMLHTTP")}),null==Object.create&&(Object.create=function(e){function t(){}return t.prototype=e,new t}),Date.now||(Date.now=function(){return(new Date).getTime()}),null==p&&(r=d,d=function(e,t){var n=t.signal;return r(e,{headers:t.headers,credentials:t.credentials,cache:t.cache}).then(function(e){var t=e.body.getReader();return n._reader=t,n._aborted&&n._reader.cancel(),{status:e.status,statusText:e.statusText,headers:e.headers,body:{getReader:function(){return t}}}})},p=function(){this.signal={_reader:null,_aborted:!1},this.abort=function(){null!=this.signal._reader&&this.signal._reader.cancel(),this.signal._aborted=!0}}),c.prototype.decode=function(e){function t(e,t,n){if(1===n)return 128>>t<=e&&e<<t<=2047;if(2===n)return 2048>>t<=e&&e<<t<=55295||57344>>t<=e&&e<<t<=65535;if(3===n)return 65536>>t<=e&&e<<t<=1114111;throw new Error}function n(e,t){if(6===e)return 15<t>>6?3:31<t?2:1;if(12===e)return 15<t?3:2;if(18===e)return 3;throw new Error}for(var r="",o=this.bitsNeeded,i=this.codePoint,a=0;a<e.length;a+=1){var s=e[a];0!==o&&(s<128||191<s||!t(i<<6|63&s,o-6,n(o,i)))&&(o=0,i=65533,r+=String.fromCharCode(i)),0===o?(i=0<=s&&s<=127?(o=0,s):192<=s&&s<=223?(o=6,31&s):224<=s&&s<=239?(o=12,15&s):240<=s&&s<=247?(o=18,7&s):(o=0,65533),0===o||t(i,o,n(o,i))||(o=0,i=65533)):(o-=6,i=i<<6|63&s),0===o&&(i<=65535?r+=String.fromCharCode(i):r=(r+=String.fromCharCode(55296+(i-65535-1>>10)))+String.fromCharCode(56320+(i-65535-1&1023)))}return this.bitsNeeded=o,this.codePoint=i,r};function u(){}null!=h&&null!=s&&function(){try{return"test"===(new h).decode((new s).encode("test"),{stream:!0})}catch(e){}return!1}()||(h=c);function I(e){this.withCredentials=!1,this.readyState=0,this.status=0,this.statusText="",this.responseText="",this.onprogress=u,this.onload=u,this.onerror=u,this.onreadystatechange=u,this._contentType="",this._xhr=e,this._sendTimeout=0,this._abort=u}function l(e){return e.replace(/[A-Z]/g,function(e){return String.fromCharCode(e.charCodeAt(0)+32)})}function f(e){for(var t=Object.create(null),n=e.split("\r\n"),r=0;r<n.length;r+=1){var o=n[r].split(": "),i=o.shift(),o=o.join(": ");t[l(i)]=o}this._map=t}function P(){}function y(e){this._headers=e}function L(){}function M(){this._listeners=Object.create(null)}function b(e){H(function(){throw e},0)}function v(e){this.type=e,this.target=void 0}function $(e,t){v.call(this,e),this.data=t.data,this.lastEventId=t.lastEventId}function q(e,t){v.call(this,e),this.status=t.status,this.statusText=t.statusText,this.headers=t.headers}function F(e,t){v.call(this,e),this.error=t.error}I.prototype.open=function(e,t){this._abort(!0);var o=this,i=this._xhr,a=1,n=0,r=(this._abort=function(e){0!==o._sendTimeout&&(N(o._sendTimeout),o._sendTimeout=0),1!==a&&2!==a&&3!==a||(a=4,i.onload=u,i.onerror=u,i.onabort=u,i.onprogress=u,i.onreadystatechange=u,i.abort(),0!==n&&(N(n),n=0),e||(o.readyState=4,o.onabort(null),o.onreadystatechange())),a=0},function(){if(1===a){var t=0,n="",r=void 0;if("contentType"in i)t=200,n="OK",r=i.contentType;else try{t=i.status,n=i.statusText,r=i.getResponseHeader("Content-Type")}catch(e){n="",r=void(t=0)}0!==t&&(a=2,o.readyState=2,o.status=t,o.statusText=n,o._contentType=r,o.onreadystatechange())}}),s=function(){if(r(),2===a||3===a){a=3;var e="";try{e=i.responseText}catch(e){}o.readyState=3,o.responseText=e,o.onprogress()}},c=function(e,t){if(null!=t&&null!=t.preventDefault||(t={preventDefault:u}),s(),1===a||2===a||3===a){if(a=4,0!==n&&(N(n),n=0),o.readyState=4,"load"===e)o.onload(t);else if("error"===e)o.onerror(t);else{if("abort"!==e)throw new TypeError;o.onabort(t)}o.onreadystatechange()}},l=function(){n=H(function(){l()},500),3===i.readyState&&s()};"onload"in i&&(i.onload=function(e){c("load",e)}),"onerror"in i&&(i.onerror=function(e){c("error",e)}),"onabort"in i&&(i.onabort=function(e){c("abort",e)}),"onprogress"in i&&(i.onprogress=s),"onreadystatechange"in i&&(i.onreadystatechange=function(e){e=e,null!=i&&(4===i.readyState?"onload"in i&&"onerror"in i&&"onabort"in i||c(""===i.responseText?"error":"load",e):3===i.readyState?"onprogress"in i||s():2===i.readyState&&r())}),!("contentType"in i)&&"ontimeout"in j.prototype||(t+=(-1===t.indexOf("?")?"?":"&")+"padding=true"),i.open(e,t,!0),"readyState"in i&&(n=H(function(){l()},0))},I.prototype.abort=function(){this._abort(!1)},I.prototype.getResponseHeader=function(e){return this._contentType},I.prototype.setRequestHeader=function(e,t){var n=this._xhr;"setRequestHeader"in n&&n.setRequestHeader(e,t)},I.prototype.getAllResponseHeaders=function(){return null!=this._xhr.getAllResponseHeaders&&this._xhr.getAllResponseHeaders()||""},I.prototype.send=function(){var e;if("ontimeout"in j.prototype&&("sendAsBinary"in j.prototype||"mozAnon"in j.prototype)||null==i||null==i.readyState||"complete"===i.readyState){var t=this._xhr;"withCredentials"in t&&(t.withCredentials=this.withCredentials);try{t.send(void 0)}catch(e){throw e}}else(e=this)._sendTimeout=H(function(){e._sendTimeout=0,e.send()},4)},f.prototype.get=function(e){return this._map[l(e)]},null!=j&&null==j.HEADERS_RECEIVED&&(j.HEADERS_RECEIVED=2),P.prototype.open=function(o,i,t,n,e,r,a){o.open("GET",e);var s,c=0;for(s in o.onprogress=function(){var e=o.responseText.slice(c);c+=e.length,t(e)},o.onerror=function(e){e.preventDefault(),n(new Error("NetworkError"))},o.onload=function(){n(null)},o.onabort=function(){n(null)},o.onreadystatechange=function(){var e,t,n,r;o.readyState===j.HEADERS_RECEIVED&&(e=o.status,t=o.statusText,n=o.getResponseHeader("Content-Type"),r=o.getAllResponseHeaders(),i(e,t,n,new f(r)))},o.withCredentials=r,a)Object.prototype.hasOwnProperty.call(a,s)&&o.setRequestHeader(s,a[s]);return o.send(),o},y.prototype.get=function(e){return this._headers.get(e)},L.prototype.open=function(e,t,o,n,r,i,a){var s=null,c=new p,l=c.signal,u=new h;return d(r,{headers:a,credentials:i?"include":"same-origin",signal:l,cache:"no-store"}).then(function(e){return s=e.body.getReader(),t(e.status,e.statusText,e.headers.get("Content-Type"),new y(e.headers)),new w(function(t,n){function r(){s.read().then(function(e){e.done?t(void 0):(e=u.decode(e.value,{stream:!0}),o(e),r())}).catch(function(e){n(e)})}r()})}).catch(function(e){if("AbortError"!==e.name)return e}).then(function(e){n(e)}),{abort:function(){null!=s&&s.cancel(),c.abort()}}},M.prototype.dispatchEvent=function(e){var t=(e.target=this)._listeners[e.type];if(null!=t)for(var n=t.length,r=0;r<n;r+=1){var o=t[r];try{"function"==typeof o.handleEvent?o.handleEvent(e):o.call(this,e)}catch(e){b(e)}}},M.prototype.addEventListener=function(e,t){e=String(e);for(var n=this._listeners,r=n[e],o=(null==r&&(n[e]=r=[]),!1),i=0;i<r.length;i+=1)r[i]===t&&(o=!0);o||r.push(t)},M.prototype.removeEventListener=function(e,t){e=String(e);var n=this._listeners,r=n[e];if(null!=r){for(var o=[],i=0;i<r.length;i+=1)r[i]!==t&&o.push(r[i]);0===o.length?delete n[e]:n[e]=o}},$.prototype=Object.create(v.prototype),q.prototype=Object.create(v.prototype),F.prototype=Object.create(v.prototype);var X=-1,G=0,V=1,B=2,k=-1,z=0,K=1,J=2,W=3,Y=/^text\/event\-stream(;.*)?$/i,E=1e3,m=18e6,Q=function(e,t){e=null==e?t:parseInt(e,10);return U(e=e!=e?t:e)},U=function(e){return Math.min(Math.max(e,E),m)},Z=function(e,t,n){try{"function"==typeof t&&t.call(e,n)}catch(e){b(e)}};function g(e,t){function i(e,t,n,r){var o;m===G&&(200===e&&null!=n&&Y.test(n)?(m=V,y=Date.now(),f=d,c.readyState=V,o=new q("open",{status:e,statusText:t,headers:r}),c.dispatchEvent(o),Z(c,c.onopen,o)):(200!==e?t=t&&t.replace(/\s+/g," "):null!=n&&n.replace(/\s+/g," "),O(),o=new q("error",{status:e,statusText:t,headers:r}),c.dispatchEvent(o),Z(c,c.onerror,o)))}function a(e){if(m===V){for(var t=-1,n=0;n<e.length;n+=1)(a=e.charCodeAt(n))!=="\n".charCodeAt(0)&&a!=="\r".charCodeAt(0)||(t=n);var r=(-1!==t?S:"")+e.slice(0,t+1);S=(-1===t?S:"")+e.slice(t+1),""!==e&&(y=Date.now(),v+=e.length);for(var o=0;o<r.length;o+=1){var i,a=r.charCodeAt(o);if(x===k&&a==="\n".charCodeAt(0))x=z;else if(x===k&&(x=z),a==="\r".charCodeAt(0)||a==="\n".charCodeAt(0)){if(x!==z&&(x===K&&(R=o+1),s=r.slice(A,R-1),i=r.slice(R+(R<o&&r.charCodeAt(R)===" ".charCodeAt(0)?1:0),o),"data"===s?C=C+"\n"+i:"id"===s?T=i:"event"===s?_=i:"retry"===s?(d=Q(i,d),f=d):"heartbeatTimeout"===s&&(h=Q(i,h),0!==E&&(N(E),E=H(function(){D()},h)))),x===z){if(""!==C){p=T;var s=new $(_=""===_?"message":_,{data:C.slice(1),lastEventId:T});if(c.dispatchEvent(s),"open"===_?Z(c,c.onopen,s):"message"===_?Z(c,c.onmessage,s):"error"===_&&Z(c,c.onerror,s),m===B)return}_=C=""}x=a==="\r".charCodeAt(0)?k:z}else x===z&&(A=o,x=K),x===K?a===":".charCodeAt(0)&&(R=o+1,x=J):x===J&&(x=W)}}}function s(e){m!==V&&m!==G||(m=X,0!==E&&(N(E),E=0),E=H(function(){D()},f),f=U(Math.min(16*d,2*f)),c.readyState=G,e=new F("error",{error:e}),c.dispatchEvent(e),Z(c,c.onerror,e))}var c,l,u,d,h,p,f,y,v,n,g,w,b,E,m,C,T,_,S,x,A,R,O,D;M.call(this),t=t||{},this.onopen=void 0,this.onmessage=void 0,this.onerror=void 0,this.url=void 0,this.readyState=void 0,this.withCredentials=void 0,this.headers=void 0,this._close=void 0,c=this,l=e,e=t,l=String(l),t=Boolean(e.withCredentials),u=e.lastEventIdQueryParameterName||"lastEventId",d=U(1e3),h=Q(e.heartbeatTimeout,45e3),p="",f=d,y=!1,v=0,n=e.headers||{},e=e.Transport,g=ee&&null==e?void 0:new I(new(null!=e?e:null!=j&&"withCredentials"in j.prototype||null==o?j:o)),w=new(null!=e&&"string"!=typeof e?e:null==g?L:P),b=void 0,m=X,S=_=T=C="",x=z,R=A=E=0,O=function(){m=B,null!=b&&(b.abort(),b=void 0),0!==E&&(N(E),E=0),c.readyState=B},D=function(){if(E=0,m!==X)y||null==b?(e=Math.max((y||Date.now())+h-Date.now(),1),y=!1,E=H(function(){D()},e)):(s(new Error("No activity within "+h+" milliseconds. "+(m===G?"No response received.":v+" chars received.")+" Reconnecting.")),null!=b&&(b.abort(),b=void 0));else{y=!1,v=0,E=H(function(){D()},h),m=G,T=p,S=_=C="",R=A=0,x=z;var e=l,t=("data:"!==l.slice(0,5)&&"blob:"!==l.slice(0,5)&&""!==p&&(e=-1===(t=l.indexOf("?"))?l:l.slice(0,t+1)+l.slice(t+1).replace(/(?:^|&)([^=&]*)(?:=[^&]*)?/g,function(e,t){return t===u?"":e}),e+=(-1===l.indexOf("?")?"?":"&")+u+"="+encodeURIComponent(p)),c.withCredentials),n={Accept:"text/event-stream"},r=c.headers;if(null!=r)for(var o in r)Object.prototype.hasOwnProperty.call(r,o)&&(n[o]=r[o]);try{b=w.open(g,i,a,s,e,t,n)}catch(e){throw O(),e}}},c.url=l,c.readyState=G,c.withCredentials=t,c.headers=n,c._close=O,D()}var ee=null!=d&&null!=a&&"body"in a.prototype;(g.prototype=Object.create(M.prototype)).CONNECTING=G,g.prototype.OPEN=V,g.prototype.CLOSED=B,g.prototype.close=function(){this._close()},g.CONNECTING=G,g.OPEN=V,g.CLOSED=B,g.prototype.withCredentials=void 0;var C=n;null==j||null!=n&&"withCredentials"in n.prototype||(C=g),a=function(e){e.EventSourcePolyfill=g,e.NativeEventSource=n,e.EventSource=C},"object"==typeof module&&"object"==typeof module.exports?a(exports):"function"==typeof define&&define.amd?define(["exports"],a):a(e)}("undefined"==typeof globalThis?"undefined"!=typeof window?window:"undefined"!=typeof self?self:this:globalThis);

/**
 * https://unpkg.com/whatwg-fetch@3.3.1/dist/fetch.umd.js
 */
(function (global, factory) {
  typeof exports === 'object' && typeof module !== 'undefined' ? factory(exports) :
  typeof define === 'function' && define.amd ? define(['exports'], factory) :
  (factory((global.WHATWGFetch = {})));
}(this, (function (exports) { 'use strict';

  var global = (typeof self !== 'undefined' && self) || (typeof global !== 'undefined' && global);

  var support = {
    searchParams: 'URLSearchParams' in global,
    iterable: 'Symbol' in global && 'iterator' in Symbol,
    blob:
      'FileReader' in global &&
      'Blob' in global &&
      (function() {
        try {
          new Blob();
          return true
        } catch (e) {
          return false
        }
      })(),
    formData: 'FormData' in global,
    arrayBuffer: 'ArrayBuffer' in global
  };

  function isDataView(obj) {
    return obj && DataView.prototype.isPrototypeOf(obj)
  }

  if (support.arrayBuffer) {
    var viewClasses = [
      '[object Int8Array]',
      '[object Uint8Array]',
      '[object Uint8ClampedArray]',
      '[object Int16Array]',
      '[object Uint16Array]',
      '[object Int32Array]',
      '[object Uint32Array]',
      '[object Float32Array]',
      '[object Float64Array]'
    ];

    var isArrayBufferView =
      ArrayBuffer.isView ||
      function(obj) {
        return obj && viewClasses.indexOf(Object.prototype.toString.call(obj)) > -1
      };
  }

  function normalizeName(name) {
    if (typeof name !== 'string') {
      name = String(name);
    }
    if (/[^a-z0-9\-#$%&'*+.^_`|~!]/i.test(name) || name === '') {
      throw new TypeError('Invalid character in header field name')
    }
    return name.toLowerCase()
  }

  function normalizeValue(value) {
    if (typeof value !== 'string') {
      value = String(value);
    }
    return value
  }

  // Build a destructive iterator for the value list
  function iteratorFor(items) {
    var iterator = {
      next: function() {
        var value = items.shift();
        return {done: value === undefined, value: value}
      }
    };

    if (support.iterable) {
      iterator[Symbol.iterator] = function() {
        return iterator
      };
    }

    return iterator
  }

  function Headers(headers) {
    this.map = {};

    if (headers instanceof Headers) {
      headers.forEach(function(value, name) {
        this.append(name, value);
      }, this);
    } else if (Array.isArray(headers)) {
      headers.forEach(function(header) {
        this.append(header[0], header[1]);
      }, this);
    } else if (headers) {
      Object.getOwnPropertyNames(headers).forEach(function(name) {
        this.append(name, headers[name]);
      }, this);
    }
  }

  Headers.prototype.append = function(name, value) {
    name = normalizeName(name);
    value = normalizeValue(value);
    var oldValue = this.map[name];
    this.map[name] = oldValue ? oldValue + ', ' + value : value;
  };

  Headers.prototype['delete'] = function(name) {
    delete this.map[normalizeName(name)];
  };

  Headers.prototype.get = function(name) {
    name = normalizeName(name);
    return this.has(name) ? this.map[name] : null
  };

  Headers.prototype.has = function(name) {
    return this.map.hasOwnProperty(normalizeName(name))
  };

  Headers.prototype.set = function(name, value) {
    this.map[normalizeName(name)] = normalizeValue(value);
  };

  Headers.prototype.forEach = function(callback, thisArg) {
    for (var name in this.map) {
      if (this.map.hasOwnProperty(name)) {
        callback.call(thisArg, this.map[name], name, this);
      }
    }
  };

  Headers.prototype.keys = function() {
    var items = [];
    this.forEach(function(value, name) {
      items.push(name);
    });
    return iteratorFor(items)
  };

  Headers.prototype.values = function() {
    var items = [];
    this.forEach(function(value) {
      items.push(value);
    });
    return iteratorFor(items)
  };

  Headers.prototype.entries = function() {
    var items = [];
    this.forEach(function(value, name) {
      items.push([name, value]);
    });
    return iteratorFor(items)
  };

  if (support.iterable) {
    Headers.prototype[Symbol.iterator] = Headers.prototype.entries;
  }

  function consumed(body) {
    if (body.bodyUsed) {
      return Promise.reject(new TypeError('Already read'))
    }
    body.bodyUsed = true;
  }

  function fileReaderReady(reader) {
    return new Promise(function(resolve, reject) {
      reader.onload = function() {
        resolve(reader.result);
      };
      reader.onerror = function() {
        reject(reader.error);
      };
    })
  }

  function readBlobAsArrayBuffer(blob) {
    var reader = new FileReader();
    var promise = fileReaderReady(reader);
    reader.readAsArrayBuffer(blob);
    return promise
  }

  function readBlobAsText(blob) {
    var reader = new FileReader();
    var promise = fileReaderReady(reader);
    reader.readAsText(blob);
    return promise
  }

  function readArrayBufferAsText(buf) {
    var view = new Uint8Array(buf);
    var chars = new Array(view.length);

    for (var i = 0; i < view.length; i++) {
      chars[i] = String.fromCharCode(view[i]);
    }
    return chars.join('')
  }

  function bufferClone(buf) {
    if (buf.slice) {
      return buf.slice(0)
    } else {
      var view = new Uint8Array(buf.byteLength);
      view.set(new Uint8Array(buf));
      return view.buffer
    }
  }

  function Body() {
    this.bodyUsed = false;

    this._initBody = function(body) {
      /*
        fetch-mock wraps the Response object in an ES6 Proxy to
        provide useful test harness features such as flush. However, on
        ES5 browsers without fetch or Proxy support pollyfills must be used;
        the proxy-pollyfill is unable to proxy an attribute unless it exists
        on the object before the Proxy is created. This change ensures
        Response.bodyUsed exists on the instance, while maintaining the
        semantic of setting Request.bodyUsed in the constructor before
        _initBody is called.
      */
      this.bodyUsed = this.bodyUsed;
      this._bodyInit = body;
      if (!body) {
        this._bodyText = '';
      } else if (typeof body === 'string') {
        this._bodyText = body;
      } else if (support.blob && Blob.prototype.isPrototypeOf(body)) {
        this._bodyBlob = body;
      } else if (support.formData && FormData.prototype.isPrototypeOf(body)) {
        this._bodyFormData = body;
      } else if (support.searchParams && URLSearchParams.prototype.isPrototypeOf(body)) {
        this._bodyText = body.toString();
      } else if (support.arrayBuffer && support.blob && isDataView(body)) {
        this._bodyArrayBuffer = bufferClone(body.buffer);
        // IE 10-11 can't handle a DataView body.
        this._bodyInit = new Blob([this._bodyArrayBuffer]);
      } else if (support.arrayBuffer && (ArrayBuffer.prototype.isPrototypeOf(body) || isArrayBufferView(body))) {
        this._bodyArrayBuffer = bufferClone(body);
      } else {
        this._bodyText = body = Object.prototype.toString.call(body);
      }

      if (!this.headers.get('content-type')) {
        if (typeof body === 'string') {
          this.headers.set('content-type', 'text/plain;charset=UTF-8');
        } else if (this._bodyBlob && this._bodyBlob.type) {
          this.headers.set('content-type', this._bodyBlob.type);
        } else if (support.searchParams && URLSearchParams.prototype.isPrototypeOf(body)) {
          this.headers.set('content-type', 'application/x-www-form-urlencoded;charset=UTF-8');
        }
      }
    };

    if (support.blob) {
      this.blob = function() {
        var rejected = consumed(this);
        if (rejected) {
          return rejected
        }

        if (this._bodyBlob) {
          return Promise.resolve(this._bodyBlob)
        } else if (this._bodyArrayBuffer) {
          return Promise.resolve(new Blob([this._bodyArrayBuffer]))
        } else if (this._bodyFormData) {
          throw new Error('could not read FormData body as blob')
        } else {
          return Promise.resolve(new Blob([this._bodyText]))
        }
      };

      this.arrayBuffer = function() {
        if (this._bodyArrayBuffer) {
          var isConsumed = consumed(this);
          if (isConsumed) {
            return isConsumed
          }
          if (ArrayBuffer.isView(this._bodyArrayBuffer)) {
            return Promise.resolve(
              this._bodyArrayBuffer.buffer.slice(
                this._bodyArrayBuffer.byteOffset,
                this._bodyArrayBuffer.byteOffset + this._bodyArrayBuffer.byteLength
              )
            )
          } else {
            return Promise.resolve(this._bodyArrayBuffer)
          }
        } else {
          return this.blob().then(readBlobAsArrayBuffer)
        }
      };
    }

    this.text = function() {
      var rejected = consumed(this);
      if (rejected) {
        return rejected
      }

      if (this._bodyBlob) {
        return readBlobAsText(this._bodyBlob)
      } else if (this._bodyArrayBuffer) {
        return Promise.resolve(readArrayBufferAsText(this._bodyArrayBuffer))
      } else if (this._bodyFormData) {
        throw new Error('could not read FormData body as text')
      } else {
        return Promise.resolve(this._bodyText)
      }
    };

    if (support.formData) {
      this.formData = function() {
        return this.text().then(decode)
      };
    }

    this.json = function() {
      return this.text().then(JSON.parse)
    };

    return this
  }

  // HTTP methods whose capitalization should be normalized
  var methods = ['DELETE', 'GET', 'HEAD', 'OPTIONS', 'POST', 'PUT'];

  function normalizeMethod(method) {
    var upcased = method.toUpperCase();
    return methods.indexOf(upcased) > -1 ? upcased : method
  }

  function Request(input, options) {
    if (!(this instanceof Request)) {
      throw new TypeError('Please use the "new" operator, this DOM object constructor cannot be called as a function.')
    }

    options = options || {};
    var body = options.body;

    if (input instanceof Request) {
      if (input.bodyUsed) {
        throw new TypeError('Already read')
      }
      this.url = input.url;
      this.credentials = input.credentials;
      if (!options.headers) {
        this.headers = new Headers(input.headers);
      }
      this.method = input.method;
      this.mode = input.mode;
      this.signal = input.signal;
      if (!body && input._bodyInit != null) {
        body = input._bodyInit;
        input.bodyUsed = true;
      }
    } else {
      this.url = String(input);
    }

    this.credentials = options.credentials || this.credentials || 'same-origin';
    if (options.headers || !this.headers) {
      this.headers = new Headers(options.headers);
    }
    this.method = normalizeMethod(options.method || this.method || 'GET');
    this.mode = options.mode || this.mode || null;
    this.signal = options.signal || this.signal;
    this.referrer = null;

    if ((this.method === 'GET' || this.method === 'HEAD') && body) {
      throw new TypeError('Body not allowed for GET or HEAD requests')
    }
    this._initBody(body);

    if (this.method === 'GET' || this.method === 'HEAD') {
      if (options.cache === 'no-store' || options.cache === 'no-cache') {
        // Search for a '_' parameter in the query string
        var reParamSearch = /([?&])_=[^&]*/;
        if (reParamSearch.test(this.url)) {
          // If it already exists then set the value with the current time
          this.url = this.url.replace(reParamSearch, '$1_=' + new Date().getTime());
        } else {
          // Otherwise add a new '_' parameter to the end with the current time
          var reQueryString = /\?/;
          this.url += (reQueryString.test(this.url) ? '&' : '?') + '_=' + new Date().getTime();
        }
      }
    }
  }

  Request.prototype.clone = function() {
    return new Request(this, {body: this._bodyInit})
  };

  function decode(body) {
    var form = new FormData();
    body
      .trim()
      .split('&')
      .forEach(function(bytes) {
        if (bytes) {
          var split = bytes.split('=');
          var name = split.shift().replace(/\+/g, ' ');
          var value = split.join('=').replace(/\+/g, ' ');
          form.append(decodeURIComponent(name), decodeURIComponent(value));
        }
      });
    return form
  }

  function parseHeaders(rawHeaders) {
    var headers = new Headers();
    // Replace instances of \r\n and \n followed by at least one space or horizontal tab with a space
    // https://tools.ietf.org/html/rfc7230#section-3.2
    var preProcessedHeaders = rawHeaders.replace(/\r?\n[\t ]+/g, ' ');
    preProcessedHeaders.split(/\r?\n/).forEach(function(line) {
      var parts = line.split(':');
      var key = parts.shift().trim();
      if (key) {
        var value = parts.join(':').trim();
        headers.append(key, value);
      }
    });
    return headers
  }

  Body.call(Request.prototype);

  function Response(bodyInit, options) {
    if (!(this instanceof Response)) {
      throw new TypeError('Please use the "new" operator, this DOM object constructor cannot be called as a function.')
    }
    if (!options) {
      options = {};
    }

    this.type = 'default';
    this.status = options.status === undefined ? 200 : options.status;
    this.ok = this.status >= 200 && this.status < 300;
    this.statusText = 'statusText' in options ? options.statusText : '';
    this.headers = new Headers(options.headers);
    this.url = options.url || '';
    this._initBody(bodyInit);
  }

  Body.call(Response.prototype);

  Response.prototype.clone = function() {
    return new Response(this._bodyInit, {
      status: this.status,
      statusText: this.statusText,
      headers: new Headers(this.headers),
      url: this.url
    })
  };

  Response.error = function() {
    var response = new Response(null, {status: 0, statusText: ''});
    response.type = 'error';
    return response
  };

  var redirectStatuses = [301, 302, 303, 307, 308];

  Response.redirect = function(url, status) {
    if (redirectStatuses.indexOf(status) === -1) {
      throw new RangeError('Invalid status code')
    }

    return new Response(null, {status: status, headers: {location: url}})
  };

  exports.DOMException = global.DOMException;
  try {
    new exports.DOMException();
  } catch (err) {
    exports.DOMException = function(message, name) {
      this.message = message;
      this.name = name;
      var error = Error(message);
      this.stack = error.stack;
    };
    exports.DOMException.prototype = Object.create(Error.prototype);
    exports.DOMException.prototype.constructor = exports.DOMException;
  }

  function fetch(input, init) {
    return new Promise(function(resolve, reject) {
      var request = new Request(input, init);

      if (request.signal && request.signal.aborted) {
        return reject(new exports.DOMException('Aborted', 'AbortError'))
      }

      var xhr = new XMLHttpRequest();

      function abortXhr() {
        xhr.abort();
      }

      xhr.onload = function() {
        var options = {
          status: xhr.status,
          statusText: xhr.statusText,
          headers: parseHeaders(xhr.getAllResponseHeaders() || '')
        };
        options.url = 'responseURL' in xhr ? xhr.responseURL : options.headers.get('X-Request-URL');
        var body = 'response' in xhr ? xhr.response : xhr.responseText;
        setTimeout(function() {
          resolve(new Response(body, options));
        }, 0);
      };

      xhr.onerror = function() {
        setTimeout(function() {
          reject(new TypeError('Network request failed'));
        }, 0);
      };

      xhr.ontimeout = function() {
        setTimeout(function() {
          reject(new TypeError('Network request failed'));
        }, 0);
      };

      xhr.onabort = function() {
        setTimeout(function() {
          reject(new exports.DOMException('Aborted', 'AbortError'));
        }, 0);
      };

      function fixUrl(url) {
        try {
          return url === '' && global.location.href ? global.location.href : url
        } catch (e) {
          return url
        }
      }

      xhr.open(request.method, fixUrl(request.url), true);

      if (request.credentials === 'include') {
        xhr.withCredentials = true;
      } else if (request.credentials === 'omit') {
        xhr.withCredentials = false;
      }

      if ('responseType' in xhr) {
        if (support.blob) {
          xhr.responseType = 'blob';
        } else if (
          support.arrayBuffer &&
          request.headers.get('Content-Type') &&
          request.headers.get('Content-Type').indexOf('application/octet-stream') !== -1
        ) {
          xhr.responseType = 'arraybuffer';
        }
      }

      if (init && typeof init.headers === 'object' && !(init.headers instanceof Headers)) {
        Object.getOwnPropertyNames(init.headers).forEach(function(name) {
          xhr.setRequestHeader(name, normalizeValue(init.headers[name]));
        });
      } else {
        request.headers.forEach(function(value, name) {
          xhr.setRequestHeader(name, value);
        });
      }

      if (request.signal) {
        request.signal.addEventListener('abort', abortXhr);

        xhr.onreadystatechange = function() {
          // DONE (success or failure)
          if (xhr.readyState === 4) {
            request.signal.removeEventListener('abort', abortXhr);
          }
        };
      }

      xhr.send(typeof request._bodyInit === 'undefined' ? null : request._bodyInit);
    })
  }

  fetch.polyfill = true;

  if (!global.fetch) {
    global.fetch = fetch;
    global.Headers = Headers;
    global.Request = Request;
    global.Response = Response;
  }

  exports.Headers = Headers;
  exports.Request = Request;
  exports.Response = Response;
  exports.fetch = fetch;

  Object.defineProperty(exports, '__esModule', { value: true });

})));















/**
 * ChildNode.append() polyfill
 * https://gomakethings.com/adding-an-element-to-the-end-of-a-set-of-elements-with-vanilla-javascript/
 * @author Chris Ferdinandi
 * @license MIT
 */
(function (elem) {

	// Check if element is a node
	// https://github.com/Financial-Times/polyfill-service
	var isNode = function (object) {

		// DOM, Level2
		if (typeof Node === 'function') {
			return object instanceof Node;
		}

		// Older browsers, check if it looks like a Node instance)
		return object &&
			typeof object === "object" &&
			object.nodeName &&
			object.nodeType >= 1 &&
			object.nodeType <= 12;

	};

	// Add append() method to prototype
	for (var i = 0; i < elem.length; i++) {
		if (!window[elem[i]] || 'append' in window[elem[i]].prototype) continue;
		window[elem[i]].prototype.append = function () {
			var argArr = Array.prototype.slice.call(arguments);
			var docFrag = document.createDocumentFragment();

			for (var n = 0; n < argArr.length; n++) {
				docFrag.appendChild(isNode(argArr[n]) ? argArr[n] : document.createTextNode(String(argArr[n])));
			}

			this.appendChild(docFrag);
		};
	}

})(['Element', 'CharacterData', 'DocumentType']);









/**
 * https://github.com/jsPolyfill/Array.prototype.find/blob/master/find.js
 */

'use strict';

Array.prototype.find = Array.prototype.find || function(callback) {
  if (this === null) {
    throw new TypeError('Array.prototype.find called on null or undefined');
  } else if (typeof callback !== 'function') {
    throw new TypeError('callback must be a function');
  }
  var list = Object(this);
  // Makes sures is always has an positive integer as length.
  var length = list.length >>> 0;
  var thisArg = arguments[1];
  for (var i = 0; i < length; i++) {
    var element = list[i];
    if ( callback.call(thisArg, element, i, list) ) {
      return element;
    }
  }
};




/**
 * Array.from() polyfill
 */
// From https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Array/from
// Production steps of ECMA-262, Edition 6, 22.1.2.1
if (!Array.from) {
	Array.from = (function () {
		var toStr = Object.prototype.toString;
		var isCallable = function (fn) {
			return typeof fn === 'function' || toStr.call(fn) === '[object Function]';
		};
		var toInteger = function (value) {
			var number = Number(value);
			if (isNaN(number)) { return 0; }
			if (number === 0 || !isFinite(number)) { return number; }
			return (number > 0 ? 1 : -1) * Math.floor(Math.abs(number));
		};
		var maxSafeInteger = Math.pow(2, 53) - 1;
		var toLength = function (value) {
			var len = toInteger(value);
			return Math.min(Math.max(len, 0), maxSafeInteger);
		};

		// The length property of the from method is 1.
		return function from(arrayLike/*, mapFn, thisArg */) {
			// 1. Let C be the this value.
			var C = this;

			// 2. Let items be ToObject(arrayLike).
			var items = Object(arrayLike);

			// 3. ReturnIfAbrupt(items).
			if (arrayLike == null) {
				throw new TypeError('Array.from requires an array-like object - not null or undefined');
			}

			// 4. If mapfn is undefined, then let mapping be false.
			var mapFn = arguments.length > 1 ? arguments[1] : void undefined;
			var T;
			if (typeof mapFn !== 'undefined') {
				// 5. else
				// 5. a If IsCallable(mapfn) is false, throw a TypeError exception.
				if (!isCallable(mapFn)) {
					throw new TypeError('Array.from: when provided, the second argument must be a function');
				}

				// 5. b. If thisArg was supplied, let T be thisArg; else let T be undefined.
				if (arguments.length > 2) {
					T = arguments[2];
				}
			}

			// 10. Let lenValue be Get(items, "length").
			// 11. Let len be ToLength(lenValue).
			var len = toLength(items.length);

			// 13. If IsConstructor(C) is true, then
			// 13. a. Let A be the result of calling the [[Construct]] internal method
			// of C with an argument list containing the single item len.
			// 14. a. Else, Let A be ArrayCreate(len).
			var A = isCallable(C) ? Object(new C(len)) : new Array(len);

			// 16. Let k be 0.
			var k = 0;
			// 17. Repeat, while k < len… (also steps a - h)
			var kValue;
			while (k < len) {
				kValue = items[k];
				if (mapFn) {
					A[k] = typeof T === 'undefined' ? mapFn(kValue, k) : mapFn.call(T, kValue, k);
				} else {
					A[k] = kValue;
				}
				k += 1;
			}
			// 18. Let putStatus be Put(A, "length", len, true).
			A.length = len;
			// 20. Return A.
			return A;
		};
	}());
}