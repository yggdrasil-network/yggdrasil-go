"use strict";
console.log("IE load fix");

var $ = function $(id) {
  return document.getElementById(id);
};
var $$ = function $$(clazz) {
  return document.getElementsByClassName(clazz);
};

function setPingValue(peer, value) {
  var cellText;
  var peerCell = $(peer);
  if (!peerCell) return;
  var peerTable = $("peer_list");
  if (value === "-1") {
    var peerAddress = $("label_" + peer);
    peerAddress.style.color = "rgba(250,250,250,.5)";
  } else {

    cellText = document.createTextNode(value);
    peerCell.appendChild(cellText);

    var peerCellTime = $("time_" + peer);
    var cellTextTime = document.createTextNode("ms");
    peerCellTime.appendChild(cellTextTime);
  }
  peerCell.parentNode.classList.remove("is-hidden");
  //sort table
  moveRowToOrderPos(peerTable, 2, peerCell.parentNode);
}

function cmpTime(a, b) {
  return a.textContent.trim() === "" ? 1 : a.textContent.trim() // using `.textContent.trim()` for test
  .localeCompare(b.textContent.trim(), 'en', { numeric: true });
}

function moveRowToOrderPos(table, col, row) {
  var tb = table.tBodies[0],
      tr = tb.rows;
  var i = 0;
  for (; i < tr.length && cmpTime(row.cells[col], tr[i].cells[col]) >= 0; ++i) {}
  if (i < tr.length && i != row.rowIndex) {
    tb.deleteRow(row.rowIndex);
    tb.insertBefore(row, tr[i]);
  }
}

function openTab(element, tabName) {
  // Declare all variables
  var i, tabContent, tabLinks;

  // Get all elements with class="content" and hide them
  tabContent = $$("tab here");
  for (i = 0; i < tabContent.length; i++) {
    tabContent[i].className = "tab here is-hidden";
  }

  // Get all elements with class="tab" and remove the class "is-active"
  tabLinks = $$("tab is-active");
  for (i = 0; i < tabLinks.length; i++) {
    tabLinks[i].className = "tab";
  }

  // Show the current tab, and add an "is-active" class to the button that opened the tab
  $(tabName).className = "tab here";
  element.parentElement.className = "tab is-active";
  //refreshRecordsList();
}

function copy2clipboard(text) {
  var textArea = document.createElement("textarea");
  textArea.style.position = 'fixed';
  textArea.style.top = 0;
  textArea.style.left = 0;

  // Ensure it has a small width and height. Setting to 1px / 1em
  // doesn't work as this gives a negative w/h on some browsers.
  textArea.style.width = '2em';
  textArea.style.height = '2em';

  // We don't need padding, reducing the size if it does flash render.
  textArea.style.padding = 0;

  // Clean up any borders.
  textArea.style.border = 'none';
  textArea.style.outline = 'none';
  textArea.style.boxShadow = 'none';

  // Avoid flash of the white box if rendered for any reason.
  textArea.style.background = 'transparent';
  textArea.value = text;

  document.body.appendChild(textArea);
  textArea.focus();
  textArea.select();
  try {
    var successful = document.execCommand('copy');
    var msg = successful ? 'successful' : 'unsuccessful';
    console.log('Copying text command was ' + msg);
  } catch (err) {
    console.log('Oops, unable to copy');
  }
  document.body.removeChild(textArea);
  showInfo('value copied successfully!');
}

function showInfo(text) {
  var info = $("notification_info");
  var message = $("info_text");
  message.innerHTML = text;

  info.className = "notification is-primary";
  var button = $("info_close");
  button.onclick = function () {
    message.value = "";
    info.className = "notification is-primary is-hidden";
  };
  setTimeout(button.onclick, 2000);
}

function showWindow(text) {
  var info = $("notification_window");
  var message = $("info_window");
  message.innerHTML = text;

  info.classList.remove("is-hidden");
  var button_info_close = $("info_win_close");
  button_info_close.onclick = function () {
    message.value = "";
    info.classList.add("is-hidden");
    $("peer_list").remove();
  };
  var button_window_close = $("window_close");
  button_window_close.onclick = function () {
    message.value = "";
    info.classList.add("is-hidden");
    $("peer_list").remove();
  };
  var button_window_save = $("window_save");
  button_window_save.onclick = function () {
    message.value = "";
    info.classList.add("is-hidden");
    //todo save peers
    var peers = document.querySelectorAll('*[id^="peer-"]');
    var peer_list = [];
    for (var i = 0; i < peers.length; ++i) {
      var p = peers[i];
      if (p.checked) {
        var peerURL = p.parentElement.parentElement.children[1].innerText;
        peer_list.push(peerURL);
      }
    }
    fetch('api/peers', {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        'Riv-Save-Config': 'true'
      },
      body: JSON.stringify(peer_list)
    }).catch(function (error) {
      console.error('Error:', error);
    });
    $("peer_list").remove();
  };
}

function add_table(peerList) {

  var peers = [];
  //const countries = Object.keys(peerList);
  // get the reference for the body
  var body = document.createElement("div");
  // creates a <table> element and a <tbody> element
  var tbl = document.createElement("table");
  tbl.setAttribute('id', "peer_list");
  //tbl.setAttribute('cellpadding', '10');
  var tblBody = document.createElement("tbody");

  // creating all cells
  for (var c in peerList) {
    var counter = 1;
    for (var peer in peerList[c]) {
      peers.push(peer);
      // creates a table row
      var row = document.createElement("tr");
      row.className = "is-hidden";
      var imgElement = document.createElement("td");
      imgElement.className = "big-flag fi fi-" + ui.lookupCountryCodeByAddress(peer);
      var peerAddress = document.createElement("td");
      var cellText = document.createTextNode(peer);
      peerAddress.appendChild(cellText);
      peerAddress.setAttribute('id', "label_" + peer);
      var peerPing = document.createElement("td");
      peerPing.setAttribute('id', peer);
      var peerPingTime = document.createElement("td");
      peerPingTime.setAttribute('id', "time_" + peer);
      var peerSelect = document.createElement("td");
      var chk = document.createElement('input');
      chk.setAttribute('type', 'checkbox');
      chk.setAttribute('id', "peer-" + counter);
      peerSelect.appendChild(chk);

      row.appendChild(imgElement);
      row.appendChild(peerAddress);
      row.appendChild(peerPing);
      row.appendChild(peerPingTime);
      row.appendChild(peerSelect);
      tblBody.appendChild(row);
    }
  }
  // put the <tbody> in the <table>
  tbl.appendChild(tblBody);
  // appends <table> into <body>
  body.appendChild(tbl);
  // sets the border attribute of tbl to 2;
  //tbl.setAttribute("border", "0");
  showWindow(body.innerHTML);
  return peers;
}

function togglePrivKeyVisibility() {
  if (this.classList.contains("fa-eye")) {
    this.classList.remove("fa-eye");
    this.classList.add("fa-eye-slash");
    $("priv_key_visible").innerHTML = $("priv_key").innerHTML;
  } else {
    this.classList.remove("fa-eye-slash");
    this.classList.add("fa-eye");
    $("priv_key_visible").innerHTML = "••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••";
  }
}

function humanReadableSpeed(speed) {
  if (speed < 0) return "? B/s";
  var i = speed < 1 ? 0 : Math.floor(Math.log(speed) / Math.log(1024));
  return (speed / Math.pow(1024, i)).toFixed(2) * 1 + ' ' + ['B/s', 'kB/s', 'MB/s', 'GB/s', 'TB/s'][i];
}

var ui = ui || {
  countries: []
};

ui.getAllPeers = function () {
  if (!("_allPeersPromise" in ui)) {
    ui._allPeersPromise = new Promise(function (resolve, reject) {
      if ("_allPeers" in ui) resolve(ui._allPeers);else fetch('https://map.rivchain.org/rest/peers.json').then(function (response) {
        return response.json();
      }).then(function (data) {
        var _loop = function _loop() {
          var country = c.slice(0, -3);
          var filtered = ui.countries.find(function (entry) {
            return entry.name.toLowerCase() == country;
          });
          //let flagFile = filtered ? filtered["flag_4x3"] : "";
          var flagCode = filtered ? filtered["code"] : "";
          for (var peer in data[c]) {
            data[c][peer].countryCode = flagCode;
          }
        };

        // add country code to each peer
        for (var c in data) {
          _loop();
        }
        ui._allPeers = data;
        resolve(ui._allPeers);
      }).catch(reject);
    }).finally(function () {
      return delete ui._allPeersPromise;
    });
  }
  return ui._allPeersPromise;
};

ui.showAllPeers = function () {
  return ui.getAllPeers().then(function (peerList) {
    var peers = add_table(peerList);
    //start peers test
    fetch('api/ping', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify(peers)
    }).catch(function (error) {
      console.error('Error:', error);
    });
  }).catch(function (error) {
    console.error(error);
  });
};

ui.getConnectedPeers = function () {
  return fetch('api/peers').then(function (response) {
    return response.json();
  });
};

ui.updateConnectedPeersHandler = function (peers) {
  ui.updateStatus(peers);
  $("peers").innerText = "";
  if (peers) {
    var regexStrip = /%[^\]]*/gm;
    peers.forEach(function (peer) {
      var row = $("peers").appendChild(document.createElement('div'));
      row.className = "overflow-ellipsis";
      var flag = row.appendChild(document.createElement("span"));
      if (peer.multicast) flag.className = "fa fa-thin fa-share-nodes peer-connected-fl";
      else flag.className = "fi fi-" + ui.lookupCountryCodeByAddress(peer.remote) + " peer-connected-fl";
      row.append(peer.remote.replace(regexStrip, ""));
    });
  }
};

ui.updateStatus = function (peers) {
  var status = "st-error";
  if (peers) {
    if (peers.length) {
      var isNonMulticastExists = peers.filter(function (peer) {
        return !peer.multicast;
      }).length;
      status = isNonMulticastExists ? "st-multicast" : "st-connected";
    } else {
      status = "st-connecting";
    }
  }
  Array.from($$("status")).forEach(function (node) {
    return node.classList.add("is-hidden");
  });
  $(status).classList.remove("is-hidden");
};

ui.updateSpeed = function (peers) {
  if (peers) {
    var rsbytes = { "bytes_recvd": peers.reduce(function (acc, peer) {
        return acc + peer.bytes_recvd;
      }, 0),
      "bytes_sent": peers.reduce(function (acc, peer) {
        return acc + peer.bytes_sent;
      }, 0),
      "timestamp": Date.now() };
    if ("_rsbytes" in ui) {
      $("dn_speed").innerText = humanReadableSpeed((rsbytes.bytes_recvd - ui._rsbytes.bytes_recvd) * 1000 / (rsbytes.timestamp - ui._rsbytes.timestamp));
      $("up_speed").innerText = humanReadableSpeed((rsbytes.bytes_sent - ui._rsbytes.bytes_sent) * 1000 / (rsbytes.timestamp - ui._rsbytes.timestamp));
    }
    ui._rsbytes = rsbytes;
  } else {
    delete ui._rsbytes;
    $("dn_speed").innerText = humanReadableSpeed(-1);
    $("up_speed").innerText = humanReadableSpeed(-1);
  }
};

ui.updateConnectedPeers = function () {
  return ui.getConnectedPeers().then(function (peers) {
    return ui.updateConnectedPeersHandler(peers);
  }).catch(function (error) {
    ui.updateConnectedPeersHandler();
    $("peers").innerText = error.message;
  });
};

ui.lookupCountryCodeByAddress = function (address) {
  for (var c in ui._allPeers) {
    for (var peer in ui._allPeers[c]) {
      if (peer == address) return ui._allPeers[c][peer].countryCode;
    }
  }
};

ui.getSelfInfo = function () {
  return fetch('api/self').then(function (response) {
    return response.json();
  });
};

ui.updateSelfInfo = function () {
  return ui.getSelfInfo().then(function (info) {
    $("ipv6").innerText = info.address;
    $("subnet").innerText = info.subnet;
    $("coordinates").innerText = ''.concat('[', info.coords.join(' '), ']');
    $("pub_key").innerText = info.key;
    $("priv_key").innerText = info.private_key;
    $("ipv6").innerText = info.address;
    $("version").innerText = info.build_version;
  }).catch(function (error) {
    $("ipv6").innerText = error.message;
  });
};

function main() {

  window.addEventListener("load", function () {
    $("showAllPeersBtn").addEventListener("click", ui.showAllPeers);

    ui.getAllPeers().then(function () {
      return ui.updateConnectedPeers();
    });

    ui.updateSelfInfo();

    ui.sse = new EventSource('/api/sse');

    ui.sse.addEventListener("ping", function (e) {
      var data = JSON.parse(e.data);
      setPingValue(data.peer, data.value);
    });

    ui.sse.addEventListener("peers", function (e) {
      ui.updateConnectedPeersHandler(JSON.parse(e.data));
    });

    ui.sse.addEventListener("rxtx", function (e) {
      ui.updateSpeed(JSON.parse(e.data));
    });

    ui.sse.addEventListener("coord", function (e) {
      var coords = JSON.parse(e.data);
      $("coordinates").innerText = ''.concat('[', coords.join(' '), ']');
    });
  });
}

main();