package com.kuaishou.agent.operation.backend;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;

import com.kuaishou.agent.operation.backend.Application;

/**
 * ApplicationTest
 *
 * Write the code. Change the world.
 *
 * @author trang
 */
class ApplicationTest extends SpringBaseTest {

    @Autowired
    private Application application;

    @Test
    void test() {
        assertThat(application).isNotNull();
    }

}