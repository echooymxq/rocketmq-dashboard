/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package org.apache.rocketmq.studio.ops.ai.tool.service;

import org.apache.rocketmq.studio.instance.InstanceResolver;
import org.apache.rocketmq.studio.provider.apache.RocketMQDefaultClusterResolver;

import org.apache.rocketmq.studio.ops.ai.tool.core.ToolExecutionException;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import org.apache.rocketmq.studio.common.domain.enums.InstanceType;
import org.apache.rocketmq.studio.ops.ai.AiToolVO;
import org.apache.rocketmq.studio.ops.ai.tool.catalog.CapabilityResolver;
import org.apache.rocketmq.studio.ops.ai.tool.catalog.ToolCatalog;
import org.apache.rocketmq.studio.instance.InstanceRepository;
import org.apache.rocketmq.studio.instance.InstanceVO;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.core.io.DefaultResourceLoader;

import java.util.Optional;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

class ToolDiscoveryServiceTest {

    private InstanceRepository instanceRepository;
    private ToolDiscoveryService discoveryService;

    @BeforeEach
    void setUp() {
        instanceRepository = mock(InstanceRepository.class);
        InstanceResolver instanceResolver =
                new InstanceResolver(instanceRepository, mock(RocketMQDefaultClusterResolver.class));
        CapabilityResolver capabilityResolver = new CapabilityResolver(instanceResolver);
        discoveryService = new ToolDiscoveryService(new ToolCatalog(new DefaultResourceLoader()),
                capabilityResolver,
                instanceResolver);
        when(instanceRepository.findByName("instance-a"))
                .thenAnswer(invocation -> instanceRepository.findByIdentifier("instance-a"));
    }

    @Test
    void discoveryWithInstanceExposesSupportedTools() {
        when(instanceRepository.findByIdentifier("instance-a"))
                .thenReturn(Optional.of(InstanceVO.builder()
                        .name("instance-a")
                        .type(InstanceType.PROXY_CLUSTER)
                        .build()));

        assertThat(discoveryService.listTools("instance-a"))
                .extracting(AiToolVO::getName)
                .contains(
                        "rmq.cluster.list",
                        "rmq.capabilities",
                        "rmq.dashboard.summary",
                        "rmq.topic.list",
                        "rmq.topic.route",
                        "rmq.topic.send",
                        "rmq.group.list",
                        "rmq.group.reset_offset",
                        "rmq.alert.rule.list")
                .contains("rmq.nameserver.config.diff")
                .contains("rmq.proxy.config_update", "rmq.lite_topic.list", "rmq.lite_topic.create");
    }

    @Test
    void discoveryWithCloudInstanceExposesOnlyCloudCompatibleTools() {
        when(instanceRepository.findByIdentifier("instance-a"))
                .thenReturn(Optional.of(InstanceVO.builder()
                        .name("instance-a")
                        .type(InstanceType.CLOUD)
                        .build()));

        var tools = discoveryService.listTools("instance-a").stream().map(AiToolVO::getName).toList();
        assertThat(tools)
                .contains("rmq.capabilities", "rmq.topic.list", "rmq.topic.create", "rmq.group.list",
                        "rmq.acl.list", "rmq.user.list", "rmq.cluster.list", "rmq.dlq.list",
                        "rmq.topic.route", "rmq.topic.send", "rmq.group.reset_offset")
                .doesNotContain("rmq.broker.list", "rmq.nameserver.config.diff",
                        "rmq.lite_topic.list", "rmq.lite_topic.create", "rmq.proxy.config_update");
    }

    @Test
    void discoveryRejectsMissingOrUnregisteredNames() {
        assertThatThrownBy(() -> discoveryService.listTools(" "))
                .isInstanceOfSatisfying(ToolExecutionException.class,
                        error -> assertThat(error.getCode()).isEqualTo(400));
        assertThatThrownBy(() -> discoveryService.listTools("unknown"))
                .isInstanceOfSatisfying(ToolExecutionException.class,
                        error -> assertThat(error.getCode()).isEqualTo(404));
    }

}
