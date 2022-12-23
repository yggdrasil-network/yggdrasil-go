package main

import (
	"log"
	"os"
	"strings"

	"github.com/RiV-chain/RiV-mesh/src/defaults"

	"github.com/docopt/docopt-go"
)

var usage = `Graphical interface for RiV mesh.

Usage:
  mesh-ui [<index>] [-c]
  mesh-ui -h | --help
  mesh-ui -v | --version

Options:
  <index>       Index file name [default: http://localhost:19019].
  -c --console  Show debug console window.
  -h --help     Show this screen.
  -v --version  Show version.`

var confui struct {
	IndexHtml string `docopt:"<index>"`
	Console   bool   `docopt:"-c,--console"`
}

var uiVersion = "0.0.1"

func main() {
	opts, _ := docopt.ParseArgs(usage, os.Args[1:], uiVersion)
	opts.Bind(&confui)
	if !confui.Console {
		Console(false)
	}
	debug := true
	w := New(debug)
	defer w.Destroy()
	w.SetTitle("RiV-mesh")
	w.SetSize(690, 920, HintFixed)

	if confui.IndexHtml == "" {
		confui.IndexHtml = defaults.GetHttpEndpoint("http://localhost:19019")
	}

	log.Printf("Opening: %v", confui.IndexHtml)
	w.SetHtml(strings.Replace(splash, "http://localhost:19019", confui.IndexHtml, 1))
	w.Run()
}

var splash = `<!DOCTYPE html>
<html>
<head>
<title>Riv mesh</title>
</head>
<script>
let ep = "http://localhost:19019";

function redirect() {
  fetch(ep + '/api')
      .then(() => {
        window.location.replace(ep);
      }).catch((error) => {
        document.getElementById("error").innerHTML = "Mesh service connection error<br>Waiting for connection....";
        setTimeout(redirect, 1000);
      });
}
redirect();

</script>
<style>
body {
  background: #333;
}

.spinner {
  position: absolute;
  width: 30px;
  height: 30px;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
}
.spinner .blob {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  border: 2px solid #fff;
  width: 10px;
  height: 10px;
  border-radius: 50%;
}
.spinner .blob.top {
  top: 0;
  -webkit-animation: blob-top 1s infinite ease-in;
          animation: blob-top 1s infinite ease-in;
}
.spinner .blob.bottom {
  top: 100%;
  -webkit-animation: blob-bottom 1s infinite ease-in;
          animation: blob-bottom 1s infinite ease-in;
}
.spinner .blob.left {
  left: 0;
  -webkit-animation: blob-left 1s infinite ease-in;
          animation: blob-left 1s infinite ease-in;
}
.spinner .move-blob {
  background: #fff;
  top: 0;
  -webkit-animation: blob-spinner-mover 1s infinite ease-in;
          animation: blob-spinner-mover 1s infinite ease-in;
}

@-webkit-keyframes blob-bottom {
  25%, 50%, 75% {
    top: 50%;
    left: 100%;
  }
  100% {
    top: 0;
    left: 50%;
  }
}

@keyframes blob-bottom {
  25%, 50%, 75% {
    top: 50%;
    left: 100%;
  }
  100% {
    top: 0;
    left: 50%;
  }
}
@-webkit-keyframes blob-left {
  25% {
    top: 50%;
    left: 0;
  }
  50%, 100% {
    top: 100%;
    left: 50%;
  }
}
@keyframes blob-left {
  25% {
    top: 50%;
    left: 0;
  }
  50%, 100% {
    top: 100%;
    left: 50%;
  }
}
@-webkit-keyframes blob-top {
  50% {
    top: 0;
    left: 50%;
  }
  75%, 100% {
    top: 50%;
    left: 0;
  }
}
@keyframes blob-top {
  50% {
    top: 0;
    left: 50%;
  }
  75%, 100% {
    top: 50%;
    left: 0;
  }
}
@-webkit-keyframes blob-spinner-mover {
  0%, 100% {
    top: 0;
    left: 50%;
  }
  25% {
    top: 50%;
    left: 100%;
  }
  50% {
    top: 100%;
    left: 50%;
  }
  75% {
    top: 50%;
    left: 0;
  }
}
@keyframes blob-spinner-mover {
  0%, 100% {
    top: 0;
    left: 50%;
  }
  25% {
    top: 50%;
    left: 100%;
  }
  50% {
    top: 100%;
    left: 50%;
  }
  75% {
    top: 50%;
    left: 0;
  }
}

#error {
  color: #fff;
  font-size: 1.5em;
  text-align: center;
  margin-top: 2em;
}
  
</style>
<body>
<div id="error"></div>
<div class="spinner">
  <div class="blob top"></div>
  <div class="blob bottom"></div>
  <div class="blob left"></div>
  
  <div class="blob move-blob"></div>
</div>

</body>
</html>
`
