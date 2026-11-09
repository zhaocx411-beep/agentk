
# agentkopbackend

## 用户须知

- 新生成的工程，用IDEA打开后，建议重新选择 Maven 插件中的 profiles，如下：
![QKAr5w](https://static.yximgs.com/bs2/infra-docs/liushilong/QKAr5w.jpg)
- 新生成的工程中，Maven 在父级 pom.xml 的最下方自动生成了 `<modules>` 标签，包含全部模块，请予以删除
![lt3qrF](https://static.yximgs.com/bs2/infra-docs/liushilong/lt3qrF.png)
- 对于生成的工程先进行 `mvn clean compile`编译，对IDEA进行依赖刷新后，再运行用例
![9X17Cx](https://static.yximgs.com/bs2/infra-docs/liushilong/9X17Cx.png)

## Runner 模块须知

### Runner demo代码
脚手架生成的runner模块代码有五个包，分别为
- kafka
- kbus
- kess.rpc
- rocketmq
- task

其内部分别有各类runner开发的 runner 实例以及主启动类（标注了@KsBootApplication注解的类），用户可根据需要启动任何一个主启动类    
示例代码中的runner启动方式均为 ksboot 非注解版本启动方式，注解与非注解版本启动方式及特点请参考文档：
[KsBoot 中runner详细解答](https://docs.corp.kuaishou.com/d/home/fcAACJq-dq8uty-4ffXSfkybp)
[KsBoot Runner 注解形式开发Demo](https://git.corp.kuaishou.com/ks/kuaishou-spring-boot-examples/-/tree/master/ks-boot-example-annotation-runner)
[KsBoot Runner 非注解形式开发 Demo](https://git.corp.kuaishou.com/ks/kuaishou-spring-boot-examples/-/tree/master/ks-boot-example-split-runner)

用户依然可以写一个主启动类启动所有类型的runner，本demo中不再描述 

### 注意事项
- 脚手架中生成的runner相关代码为demo代码，目的是告知用户各类runner的开发方式，务必不能使用示例代码中提供的topic、consumer-group 等由架构组申请的资源上线    
- 在参考demo代码开发完业务runner服务后，尽量将demo代码删除,防止扫包扫描到demo中的runner 实例
- 对各类 runner 有疑问的请咨询各 runner 类型 oncall，框架启动问题咨询 ksboot oncall
- kess-rpc 我们并不能将其作为标准的runner，因为其启动机制和其他类型runner不一致，应将其看做单独的组件类型使用。

### runner启动
- 如果服务正常启动，可通过搜索日志关键字 `Running with services`， 查看本次启动的runner有哪些，如下：
![xCt4pH](https://static.yximgs.com/bs2/infra-docs/liushilong/xCt4pH.png)
- 如果服务启动成功，但是发现runner貌似并没有正常工作，搜索启动日志关键字 `No Runner Bean or no Initializer to Run`,如果出现该日志条目，说明缺少对应的依赖或者runner实现。
- 如果发现服务启动成功，但是部分runner实例并未正常工作，此时应该去pom.xml中查询是否缺少了对应runner的依赖。


## SDK 模块须知
- SDK 模块一般都是写一些proto等可以扩散给他人使用的代码，但是坚决不允许写入复杂的业务代码
- SDK 模块不允许存在静默初始化逻辑，比如写自动配置类等，写配置类也不建议，需充分考虑一旦sdk扩散是否会对其他业务线造成影响
- [在快手开发一个业务starter需要注意什么？](https://kstack.corp.kuaishou.com/article/5739)
- [你的pipeline因为这个被阻塞了！！](https://kstack.corp.kuaishou.com/article/5819)

## 上线脚本

**IDEA直接启动主启动类为最佳本地启动方式**
![wWuYDZ](https://static.yximgs.com/bs2/infra-docs/liushilong/wWuYDZ.png)

**SDK 发布**
```bash
mvn clean deploy -P release-sdk
```

**Api 本地打包 & 测试环境运行示例**
```bash
mvn clean package -P dev -Dmaven.source.skip=true -pl agent-operation-backend-api -am
java -jar agent-operation-backend-api/target/agent-operation-backend-api.jar
```

**Runner 本地打包 & 测试环境运行示例,可以自行更改主启动类 main-class 内容**
```bash
mvn clean package -P dev -Dmaven.source.skip=true -pl agent-operation-backend-runner -am
java -Dmain-class=com.kuaishou.agent.operation.backend.runner.kafka.ExampleKafkaConsumerApplication -jar agent-operation-backend-runner/target/agent-operation-backend-runner.jar
```


## 相关文档   
* [KsBoot 中runner详细解答](https://docs.corp.kuaishou.com/d/home/fcAACJq-dq8uty-4ffXSfkybp)
* [KsBoot 中各类runner开发教程](https://halo.corp.kuaishou.com/help/framework-components/framework-components-3/best-practice/00-ksboot-cron-job.html)
* [KsBoot 文档中心](https://halo.corp.kuaishou.com/help/framework-components/framework-components-3/productintro/00-ksboot-introduce.html)
* [KsBoot 代码示例](https://git.corp.kuaishou.com/ks/kuaishou-spring-boot-examples)
* [KsBoot 常见问题](https://halo.corp.kuaishou.com/help/framework-components/framework-components-3/faq/04-ksboot-faq.html)
* [KsBoot 部署教程](https://docs.corp.kuaishou.com/d/home/fcACZ5R_YBNSILJyuIbRhHise)
* [在快手开发一个业务starter需要注意什么？](https://kstack.corp.kuaishou.com/article/5739)
* [你的pipeline因为这个被阻塞了！！](https://kstack.corp.kuaishou.com/article/5819)


## 问题反馈  
如果脚手架生成的代码有任何问题，请向ksboot oncall反馈，ksboot用户交流群
![vYu8sL](https://static.yximgs.com/bs2/infra-docs/liushilong/vYu8sL.png)
