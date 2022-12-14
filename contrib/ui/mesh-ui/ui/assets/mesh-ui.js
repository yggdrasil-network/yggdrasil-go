function setFieldValue(id, value) {
  //console.log(`setFieldValue(${id}, ${value})`);
  if(id === 'peers') {
    const reg =/%[^\]]*/gm;
    value = value.replace(reg,"");
  }
  var field = document.getElementById(id);
  field.innerHTML = value;
}

function setPingValue(peer, value) {
  var cellText;
  var peerCell = document.getElementById(peer);
  var peerTable = document.getElementById("peer_list");
  if (value === "-1") {
    var peerAddress = document.getElementById("label_" + peer);
    peerAddress.style.color = "rgba(250,250,250,.5)";
  } else {

    cellText = document.createTextNode(value);
    peerCell.appendChild(cellText);

    var peerCellTime = document.getElementById("time_" + peer);
    var cellTextTime = document.createTextNode("ms");
    peerCellTime.appendChild(cellTextTime);
  }
  peerCell.parentNode.classList.remove("is-hidden");
  //sort table
  moveRowToOrderPos(peerTable, 2, peerCell.parentNode)
}

function cmpTime(a, b) {
  return a.textContent.trim() === "" ? 1 : (a.textContent.trim() // using `.textContent.trim()` for test
    .localeCompare(b.textContent.trim(), 'en', { numeric: true }))
}

function moveRowToOrderPos(table, col, row) {
  var tb = table.tBodies[0], tr = tb.rows;
  var i = 0;
  for (; i < tr.length && cmpTime(row.cells[col], tr[i].cells[col]) >= 0; ++i);
  if (i < tr.length && i != row.rowIndex) {
    tb.deleteRow(row.rowIndex);
    tb.insertBefore(row, tr[i]);
  }

}

function openTab(element, tabName) {
  // Declare all variables
  var i, tabContent, tabLinks;

  // Get all elements with class="content" and hide them
  tabContent = document.getElementsByClassName("tab here");
  for (i = 0; i < tabContent.length; i++) {
    tabContent[i].className = "tab here is-hidden";
  }

  // Get all elements with class="tab" and remove the class "is-active"
  tabLinks = document.getElementsByClassName("tab is-active");
  for (i = 0; i < tabLinks.length; i++) {
    tabLinks[i].className = "tab";
  }

  // Show the current tab, and add an "is-active" class to the button that opened the tab
  document.getElementById(tabName).className = "tab here";
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
  var info = document.getElementById("notification_info");
  var message = document.getElementById("info_text");
  message.innerHTML = text;

  info.className = "notification is-primary";
  var button = document.getElementById("info_close");
  button.onclick = function () {
    message.value = "";
    info.className = "notification is-primary is-hidden";
  };
  setTimeout(button.onclick, 2000);
}

function showWindow(text) {
  var info = document.getElementById("notification_window");
  var message = document.getElementById("info_window");
  message.innerHTML = text;

  info.classList.remove("is-hidden");
  var button_info_close = document.getElementById("info_win_close");
  button_info_close.onclick = function () {
    message.value = "";
    info.classList.add("is-hidden");
    //document.getElementById("peer_list").remove();
  };
  var button_window_close = document.getElementById("window_close");
  button_window_close.onclick = function () {
    message.value = "";
    info.classList.add("is-hidden");
    //document.getElementById("peer_list").remove();
  };
  var button_window_save = document.getElementById("window_save");
  button_window_save.onclick = function () {
    message.value = "";
    info.classList.add("is-hidden");
    //todo save peers
    var peers = document.querySelectorAll('*[id^="peer-"]');
    var peer_list = [];
    for (i = 0; i < peers.length; ++i) {
      var p = peers[i];
      if (p.checked) {
        var peerURL = p.parentElement.parentElement.children[1].innerText;
        peer_list.push(peerURL);
      }
    }
    savePeers(JSON.stringify(peer_list));
    //document.getElementById("peer_list").remove();
  };
}

function getPeerList() {
  var xhr = new XMLHttpRequest();
  xhr.open("GET", 'https://map.rivchain.org/rest/peers.json', false);
  xhr.send(null);
  if (xhr.status === 200) {
    const peerList = JSON.parse(xhr.responseText);
    var peers = add_table(peerList);
    //start peers test
    ping(JSON.stringify(peers));
  }
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
    let counter = 1;
    flag = document.getElementById("flag_" + c.slice(0, -3).replace(/ /g, "_"));
    for (peer in peerList[c]) {
      peers.push(peer);
      // creates a table row
      var row = document.createElement("tr");
      row.className = "is-hidden";
      var imgElement = document.createElement("td");
      clone = flag.cloneNode(false);
      imgElement.appendChild(clone);
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
    document.getElementById("priv_key_visible").innerHTML = document.getElementById("priv_key").innerHTML;
  } else {
    this.classList.remove("fa-eye-slash");
    this.classList.add("fa-eye");
    document.getElementById("priv_key_visible").innerHTML = "••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••";
  }
}
