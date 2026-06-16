# 外部组 / 属性到内部角色映射草案

## 结论

MVP 仍以 GovScribe 内部 RBAC 绑定为权威，外部 IAM 只负责认证通过并提供外部用户标识与属性。终局接入 CAS / OIDC / LDAP / 国产 IAM 时，外部组或属性仅作为初始化或同步建议，写入内部 `user_roles` 后才参与授权判定。

## 映射规则

| 外部属性候选 | 内部角色 | 说明 |
| --- | --- | --- |
| `govscribe_admin` / `system_admin` | `system_admin` | 限部署管理员显式配置，不做默认映射。 |
| `secretary` / `document_secretary` | `secretary` | 文秘核心用户，可起草、核稿、采纳、范文入库。 |
| `business_user` / `department_writer` | `business_user` | 业务兼职用户，保持收窄权限。 |
| `auditor` / `security_auditor` | `auditor` | 只读审计，不授予写操作。 |

## fail-closed 口径

- 外部属性缺失、无法识别或多重冲突时，不自动授予任何角色。
- 已存在内部角色绑定时，以内部 `user_roles` 为准，不被外部属性静默覆盖。
- 外部组到内部角色的同步必须写账号安全审计日志，记录操作者、外部来源、目标账号和目标角色。
