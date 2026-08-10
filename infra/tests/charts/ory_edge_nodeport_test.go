package charts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A borda alcança Kratos e Hydra por porta publicada, não por `kubectl port-forward`.
//
// Medido no droplet app-dev em 2026-08-10, durante a reconstrução total: `auth-dev` devolvia
// 502 com Kratos e Hydra PERFEITAMENTE saudáveis no cluster. Os serviços do chart são
// ClusterIP, e a borda os alcançava por dois `kubectl port-forward` que ninguém supervisionava
// — nem unidade systemd, nem nada. Destruir os clusters matou os processos, e o login ficou
// fora até alguém reiniciá-los à mão.
//
// É exatamente o defeito do B-010, que custou ao `/engine/*` uma janela de erro a cada
// reinício do encaminhamento (NRestarts=126499, ~1,8 por minuto). Lá a resposta foi NodePort
// publicado pelo serverlb; aqui é a mesma, pelo mesmo motivo: port-forward é ferramenta de
// depuração, não transporte.
//
// Este guarda trava as TRÊS pontas juntas, porque quebrar qualquer uma reproduz o 502:
// o Service com nodePort fixo, o mapeamento de porta do cluster, e a porta que a borda
// realmente procura. Fixar uma sem a outra é o meio-conserto que já nos custou duas vezes.
type edgeExposure struct {
	nome       string // rótulo humano
	manifesto  string // caminho do Service NodePort
	nodePort   string // porta do nó, fixada no manifesto
	portaHost  string // porta publicada no host, que a borda procura
	seletorApp string // app.kubernetes.io/name do pod alvo
	portaAlvo  string // porta do Service (upstream do chart)
}

var exposicoesDaBorda = []edgeExposure{
	{
		nome:       "Kratos public",
		manifesto:  "infra/helm/ory-kratos/nodeport-service.yaml",
		nodePort:   "30005",
		portaHost:  "14433",
		seletorApp: "kratos",
		portaAlvo:  "80",
	},
	{
		nome:       "Hydra public",
		manifesto:  "infra/helm/ory-hydra/nodeport-service.yaml",
		nodePort:   "30006",
		portaHost:  "14444",
		seletorApp: "hydra",
		portaAlvo:  "4444",
	},
}

// TestOryEdgeServicesArePinnedNodePorts — o Service existe, é NodePort e a porta é FIXA.
//
// Sem fixar, o Kubernetes escolhe uma porta livre entre 30000-32767 a cada recriação, e o
// mapeamento do cluster (que é estático) deixa de apontar para ela. Um NodePort não-fixo aqui
// é um 502 esperando a próxima recriação.
func TestOryEdgeServicesArePinnedNodePorts(t *testing.T) {
	root := repoRoot(t)
	for _, e := range exposicoesDaBorda {
		t.Run(e.nome, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Clean(filepath.Join(root, e.manifesto)))
			if err != nil {
				t.Fatalf("%s não existe — sem ele a borda volta a depender de `kubectl port-forward`, "+
					"que morre com o cluster e derruba o login: %v", e.manifesto, err)
			}
			m := string(b)

			if !strings.Contains(m, "type: NodePort") {
				t.Errorf("%s não declara `type: NodePort`", e.manifesto)
			}
			if !regexp.MustCompile(`nodePort:\s*` + e.nodePort).MatchString(m) {
				t.Errorf("%s não fixa `nodePort: %s`; sem fixar, o Kubernetes sorteia a porta a cada "+
					"recriação e o mapeamento estático do cluster passa a apontar para o vazio",
					e.manifesto, e.nodePort)
			}
			// O seletor tem de casar com o pod do chart, senão o Service existe com zero
			// endpoints — que responde igualzinho a não existir, e é mais difícil de ver.
			if !regexp.MustCompile(`app\.kubernetes\.io/name:\s*` + e.seletorApp).MatchString(m) {
				t.Errorf("%s não seleciona `app.kubernetes.io/name: %s` — um Service sem endpoints "+
					"falha exatamente como um Service ausente, só que silenciosamente",
					e.manifesto, e.seletorApp)
			}
			if !regexp.MustCompile(`port:\s*` + e.portaAlvo).MatchString(m) {
				t.Errorf("%s não encaminha para a porta %s do serviço do chart", e.manifesto, e.portaAlvo)
			}
		})
	}
}

// TestCoreClusterPublishesTheEdgePorts — o cluster publica a porta de host que a borda procura.
//
// O Service NodePort sozinho não basta: o k3d só expõe no host as portas declaradas em
// `infra/k3d/core-cluster.yaml`. Um NodePort sem mapeamento é alcançável de dentro do cluster
// e de lugar nenhum mais.
func TestCoreClusterPublishesTheEdgePorts(t *testing.T) {
	root := repoRoot(t)
	b, err := os.ReadFile(filepath.Clean(filepath.Join(root, "infra/k3d/core-cluster.yaml")))
	if err != nil {
		t.Fatalf("ler core-cluster.yaml: %v", err)
	}
	cfg := string(b)

	for _, e := range exposicoesDaBorda {
		t.Run(e.nome, func(t *testing.T) {
			mapeamento := e.portaHost + ":" + e.nodePort
			if !strings.Contains(cfg, mapeamento) {
				t.Errorf("core-cluster.yaml não publica `%s` — o Service NodePort %s existiria "+
					"sem ninguém conseguir alcançá-lo do host, que é onde a borda roda",
					mapeamento, e.nodePort)
			}
		})
	}
}

// TestEdgeConfigUsesThePublishedPorts fecha o triângulo: a borda procura a porta que o
// cluster publica.
//
// Este é o elo que ninguém verifica de olho, porque as três pontas moram em arquivos
// diferentes — o Service num chart, o mapeamento num YAML de cluster, e a porta num Caddyfile.
// Foi a divergência entre eles que produziu o 502 com tudo saudável.
func TestEdgeConfigUsesThePublishedPorts(t *testing.T) {
	root := repoRoot(t)
	// O Caddyfile canônico é versionado, então ausência aqui é defeito e não ambiente:
	// pular tornaria este elo — o único que amarra a borda ao cluster — invisível
	// exatamente quando ele some.
	caddy := filepath.Join(root, "infra/local/Caddyfile")
	b, err := os.ReadFile(filepath.Clean(caddy))
	if err != nil {
		t.Fatalf("Caddyfile canônico ausente em %s: %v", caddy, err)
	}
	cfg := string(b)
	for _, e := range exposicoesDaBorda {
		t.Run(e.nome, func(t *testing.T) {
			if !strings.Contains(cfg, "localhost:"+e.portaHost) {
				t.Errorf("o Caddyfile não encaminha para `localhost:%s`; se a borda procura outra "+
					"porta, publicar esta não devolve o login", e.portaHost)
			}
		})
	}
}

// TestEdgeDoesNotProxyToAPortForward fecha o B-010 onde ele ficou aberto.
//
// O B-010 trocou o transporte do loopzero-cloud de `kubectl port-forward` para NodePort
// publicado — mas só no droplet. O Caddyfile canônico deste repositório seguiu apontando
// para `localhost:13002`, a porta do encaminhamento antigo, com o comentário
// "loopzero-cloud via port-forward" ainda no cabeçalho.
//
// O custo apareceu em 2026-08-10: sincronizar o arquivo canônico por cima do servidor
// REVERTEU o conserto, e `/engine/*` voltou de 401 para 502 — nada escutava na 13002.
// Um conserto que vive só no servidor não é conserto; é uma correção esperando o próximo
// `git pull` para ser desfeita.
func TestEdgeDoesNotProxyToAPortForward(t *testing.T) {
	root := repoRoot(t)
	b, err := os.ReadFile(filepath.Clean(filepath.Join(root, "infra/local/Caddyfile")))
	if err != nil {
		t.Fatalf("ler Caddyfile: %v", err)
	}
	cfg := string(b)

	if strings.Contains(cfg, "localhost:13002") {
		t.Error("o Caddyfile encaminha para localhost:13002 — a porta do `kubectl port-forward` " +
			"que o B-010 aposentou. Ninguém escuta nela; o cluster publica o loopzero-cloud em 3002 " +
			"pelo loadbalancer. Sincronizar este arquivo por cima do servidor reverte o conserto.")
	}
	if !strings.Contains(cfg, "localhost:3002") {
		t.Error("o Caddyfile não encaminha para localhost:3002, que é a porta publicada do loopzero-cloud")
	}
}
