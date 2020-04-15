# Yggdrasil

[![CircleCI](https://circleci.com/gh/yggdrasil-network/yggdrasil-go.svg?style=shield&circle-token=:circle-token
)](https://circleci.com/gh/yggdrasil-network/yggdrasil-go)

## Introdução

O Yggdrasil é uma implementação inicial de uma rede IPv6 totalmente criptografada de ponta a ponta. É leve, auto-organizado, é suportado em várias plataformas e permite que praticamente qualquer aplicativo compatível com IPv6 se comunique de forma segura com outros nós do Yggdrasil. O Yggdrasil não exige que você tenha conectividade com a Internet IPv6 - ele também funciona com IPv4.

Embora Yggdrasil compartilhe muitas semelhanças com [cjdns](https://github.com/cjdelisle/cjdns), emprega um algoritmo de roteamento diferente com base em uma árvore de abrangência globalmente acordada e roteamento ganancioso em um espaço métrico, e visa implementar algumas novas técnicas de roteamento de contrapressão local. Em teoria, o Yggdrasil deve escalar bem em redes com topologias semelhantes à Internet.

## Plataformas Suportadas

Apoiamos ativamente as seguintes plataformas, e pacotes estão disponíveis para algumas das opções abaixo:

- Linux
  - Os pacotes`.deb` e `.rpm` são criados pelo CI para distribuições baseadas no Debian e Red Hat
  - Pacotes Void e Arch também disponíveis em seus respectivos repositórios
- macOS
  - Pacotes`.pkg` são criados pelo CI
- Ubiquiti EdgeOS
  - `.deb` Pacotes Vyatta são criados pelo CI
- Windows
- FreeBSD
- OpenBSD
- OpenWrt

Consulte nossas páginas [Platforms](https://yggdrasil-network.github.io/platforms.html) para obter mais
informações específicas sobre cada uma de nossas plataformas suportadas, incluindo
etapas de instalação e advertências.

Você também pode encontrar outros wrappers, scripts ou ferramentas específicos da plataforma na pasta `contrib`.

## Compilando

Se você deseja construir a partir do código-fonte, em vez de instalar um dos
pacotes:

1. Instale [Go](https://golang.org) (requer Go 1.13 ou posterior)
2. Clonar este repositório
2. Execute `./build`

Observe que você pode compilar de forma cruzada para outras plataformas e arquiteturas especificando as variáveis de ambiente `GOOS` and `GOARCH`, por exemplo `GOOS=windows
./build` ou `GOOS=linux GOARCH=mipsle ./build`.

## Executando

### Gerar configuração

Para gerar configuração estática, gere um arquivo HJSON (compatível com humanos, completo com comentários):

```
./yggdrasil -genconf > /path/to/yggdrasil.conf
```

... ou gere um arquivo JSON simples (fácil de manipular
programaticamente):

```
./yggdrasil -genconf -json > /path/to/yggdrasil.conf
```

Você precisará editar o arquivo `yggdrasil.conf` para adicionar ou remover pares, modificar outras configurações, como endereços de escuta ou endereços multicast, etc.

### Execute o Yggdrasil

Para executar com a configuração estática gerada:
```
./yggdrasil -useconffile /path/to/yggdrasil.conf
```

Para executar no modo de configuração automática (que usará padrões sãos e chaves aleatórias
em cada inicialização, em vez de usar um arquivo de configuração estática):

```
./yggdrasil -autoconf
```

Você provavelmente precisará executar o Yggdrasil como um usuário privilegiado ou em `sudo`,
a menos que você tenha permissão para criar adaptadores TUN/TAP. No Linux, isso pode ser feito
dando ao binário Yggdrasil a capacidade `CAP_NET_ADMIN`.

## Documentação

A documentação está disponível em nosso [GitHub
Pages](https://yggdrasil-network.github.io) ou no submódulo base
repositório dentro de `doc / yggdrasil-network.github.io`.

- [Opções do arquivo de configuração](https://yggdrasil-network.github.io/configuration.html)
- [Documentação específica da plataforma](https://yggdrasil-network.github.io/platforms.html)
- [Perguntas frequentes](https://yggdrasil-network.github.io/faq.html)
- [Documentação da API do administrador](https://yggdrasil-network.github.io/admin.html)
- [Version changelog](CHANGELOG.md)

## Comunidade

Sinta-se livre para se juntar a nós em nosso [canal Matrix](https://matrix.to/#/#yggdrasil:matrix.org) em `#yggdrasil:matrix.org`
ou no canal de IRC `#yggdrasil` no Freenode.

## Licença

Este código é lançado sob os termos do LGPLv3, mas com uma exceção adicional que foi descaradamente retirada de [godeb](https://github.com/niemeyer/godeb).
Sob certas circunstâncias, essa exceção permite a distribuição de binários que estão (estaticamente ou dinamicamente) vinculados a esse código, sem exigir a distribuição do Minimal Corresponding Source ou Minimal Application Code.
Para mais detalhes, consulte: [LICENSE](LICENSE).
