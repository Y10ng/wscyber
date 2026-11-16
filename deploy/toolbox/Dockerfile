# 单一共享运行镜像：本项目 Go 程序、静态前端与已确认的工具层。
# 保留历史配方路径；从项目根目录通过 make image 构建，不下载工具或执行来源入口。
# 基础层来源/ID 与许可边界见 docs/components.md；仅本机使用，不声明可公开分发。
FROM wscyber/toolbox-base:local
LABEL org.opencontainers.image.title="wscyber runtime" io.wscyber.component="runtime"

# COPY 仅接收宿主已构建产物；CGO_ENABLED=0 避免依赖宿主 libc。
WORKDIR /opt/wscyber
COPY --chmod=0555 bin/runtime/wscyber /opt/wscyber/bin/wscyber
COPY web/dist-react/ /opt/wscyber/web/

ENV HOME=/tmp \
    PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    WSCYBER_LISTEN=0.0.0.0:8790 \
    WSCYBER_WEB_DIR=/opt/wscyber/web \
    WSCYBER_SANDBOX_IMAGE=wscyber/runtime:local
USER 65534:65534
EXPOSE 8790

# 显式清除基础层的入口、默认命令及健康检查；沙箱执行器另外覆盖入口与环境。
HEALTHCHECK NONE
ENTRYPOINT ["/opt/wscyber/bin/wscyber"]
CMD []

# PostgreSQL/凭据仍由外部配置提供，不写入镜像。
# 容器内控制面执行命令还需 Docker CLI 与 /var/run/docker.sock 访问权限。
# 本配方未安装/验证 CLI、不挂载 socket，也不是已完成的容器部署；宿主 make run 保留。
